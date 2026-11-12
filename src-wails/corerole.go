//go:build !headless

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// Plan 009: the backend (App) runs as its own process — `Burrow --core` —
// and the window process is only a client of its /v2/ws, like the phone.

// coreDetachedEnv marks a core that was started through spawnDetached.
const coreDetachedEnv = "BURROW_CORE_DETACHED"

func coreRoleRequested() bool {
	for _, a := range os.Args[1:] {
		if a == "--core" {
			return true
		}
	}
	return false
}

// runCore is the backend with no window: what main_headless.go does, minus
// forcing remote access on (the desktop's Settings owns that toggle).
func runCore() {
	// An `mcp` build (just dev-mcp) starts Wails' MCP server in the UI AND here,
	// and a taken port is fatal: the UI holds 9099, so the core takes any free
	// one instead of exiting before it ever serves the UI.
	if os.Getenv("WAILS_MCP_PORT") == "" {
		os.Setenv("WAILS_MCP_PORT", "0")
	}
	dataDir, err := appDataDir()
	if err != nil {
		log.Fatalf("core: app data dir: %v", err)
	}
	// Started by `burrow` inside a terminal tab (or any shell): hand ourselves
	// to the launchd launcher, or closing that tab would take the core down
	// with its process group. The UI's ensureCore already goes through it.
	if os.Getenv(coreDetachedEnv) == "" {
		if exe, err := os.Executable(); err == nil {
			_ = os.MkdirAll(dataDir, 0o755)
			env := append(os.Environ(), coreDetachedEnv+"=1")
			if spawnDetached("core", []string{exe, "--core"}, env, filepath.Join(dataDir, "core.log"), filepath.Join(dataDir, "hook.port")) == nil {
				return
			}
		}
	}
	// Not inherited by the daemon, its shells and chats: a `burrow` started in
	// one of them must see a core it starts as undetached.
	os.Unsetenv(coreDetachedEnv)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("core: %v", err)
	}
	// One core per data dir: a UI and a CLI racing to start one must not get
	// two backends writing the same SQLite and config.json.
	lock, err := os.OpenFile(filepath.Join(dataDir, "core.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		log.Fatalf("core: lock: %v", err)
	}
	// Held for the process's life. LOAD-BEARING: a local would be collected and
	// its finalizer would close the fd — dropping the flock, so a second core
	// (an update restart racing the UI's ensureCore) started next to this one.
	coreLock = lock
	// Wait a little: a core restarting onto an update starts us before it
	// has finished shutting down.
	for i := 0; ; i++ {
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			break
		}
		if i == 150 { // 15 s
			log.Printf("core: another core owns %s, exiting", dataDir)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Where `burrow` finds this binary to start a core again after an idle exit.
	if exe, err := os.Executable(); err == nil {
		_ = os.WriteFile(filepath.Join(dataDir, "core.exe"), []byte(exe), 0o644)
	}
	app := NewApp()
	coreApp = app
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	// A Wails app with no main window and no Dock icon: it owns the tray, the
	// tray panel (a window of its own, served the in-process ticket) and the
	// global hotkey. The windows live in the UI process.
	notifier := notifications.New()
	wapp = application.New(application.Options{
		Name: "Burrow",
		Services: []application.Service{
			application.NewService(&DesktopService{a: app}),
			application.NewService(notifier),
		},
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(dist)},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory,
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		ShouldQuit: func() bool {
			quitting.Store(true)
			return true
		},
		OnShutdown: app.cleanupOnShutdown,
	})
	app.startup(context.Background())
	setupTray(app)
	setupNativeNotifications(notifier)
	coreQuit = func() { quitApp(app) } // tray Quit, ⌥⌘Q in the UI: tells the UI, then stops
	go app.runCoreLifecycle(func() { wapp.Quit() }, func() {
		// Start the new binary first; it waits for this one's lock. A UI sees
		// the socket drop and reconnects (no core-quit: it must stay open).
		if exe, err := os.Executable(); err == nil {
			_ = spawnDetached("core", []string{exe, "--core"}, append(os.Environ(), coreDetachedEnv+"=1"),
				filepath.Join(dataDir, "core.log"), filepath.Join(dataDir, "hook.port"))
		}
		wapp.Quit()
	})
	var uiOpenMu sync.Mutex // three verbs at once must not start three windows
	ensureUIOpen = func() {
		uiOpenMu.Lock()
		defer uiOpenMu.Unlock()
		if app.remoteWS.uiConnected() {
			return
		}
		openUI("")
		for i := 0; i < 100 && !app.remoteWS.uiConnected(); i++ { // ≤ 10 s
			time.Sleep(100 * time.Millisecond)
		}
		// ponytail: the socket is up before App.vue has mounted the control
		// bridge, and an event with no listener yet is dropped; 2 s covers a
		// cold start. A "bridge ready" handshake would make it exact.
		time.Sleep(2 * time.Second)
	}
	log.Printf("core up (pid %d, hook port %d)", os.Getpid(), app.hookPort)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		log.Printf("core: shutting down")
		wapp.Quit()
	}()
	if err := wapp.Run(); err != nil {
		log.Fatal(err)
	}
}

// setupNativeNotifications makes the core the one poster of OS notifications
// (plan 009): same decisions as the phone push (pushtrigger.go); a click
// opens the UI at the chat / tab.
func setupNativeNotifications(n *notifications.NotificationService) {
	send := func(title, body, url, tag string) {
		err := n.SendNotification(notifications.NotificationOptions{
			ID:    tag + "-" + strconv.FormatInt(time.Now().UnixNano(), 36),
			Title: title,
			Body:  body,
			Data:  map[string]interface{}{"url": url},
		})
		if err != nil {
			log.Printf("core: notify: %v", err)
		}
	}
	// Only an authorized core takes notifications over from the webview.
	authorize := func(ok bool) string {
		if ok {
			nativeNotifier.Store(&send)
			return "granted"
		}
		nativeNotifier.Store(nil)
		return "denied"
	}
	wapp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			ok, err := n.RequestNotificationAuthorization()
			if !ok {
				log.Printf("core: notifications not authorized (%v); the window posts them", err)
			}
			authorize(ok)
		}()
	})
	n.OnNotificationResponse(func(r notifications.NotificationResult) {
		if r.Error != nil {
			return
		}
		url, _ := r.Response.UserInfo["url"].(string)
		openUI(strings.TrimPrefix(url, "#"))
	})
	nativeNotifyStatus = func() string {
		ok, err := n.CheckNotificationAuthorization()
		switch {
		case ok:
			authorize(true) // granted later in System Settings
			return "granted"
		case err != nil:
			return "denied"
		}
		return "not_determined"
	}
	nativeNotifyRequest = func() string {
		ok, _ := n.RequestNotificationAuthorization()
		return authorize(ok)
	}
}

// askCoreToQuit is the UI's ⌥⌘Q: quit completely, core first.
func askCoreToQuit() {
	dataDir, err := appDataDir()
	if err != nil {
		return
	}
	port, _ := os.ReadFile(filepath.Join(dataDir, "hook.port"))
	token, _ := os.ReadFile(filepath.Join(dataDir, "control.token"))
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%s/v2/core-quit", strings.TrimSpace(string(port))), nil)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	if resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req); err == nil {
		resp.Body.Close()
	}
}

// coreLock keeps core.lock's fd (and so its flock) alive; see runCore.
var coreLock *os.File

// coreApp is set in the core role: what the tray opens the UI for.
var coreApp *App

// openUI brings the desktop UI forward at route ("" = wherever it was). A UI
// process already connected gets it over its socket (App.vue); otherwise one
// is started with the route as its first URL. Not `open -b`: this core runs
// from the same bundle, so LaunchServices would hand the request to the core.
// ponytail: a burrow:// URL scheme replaces this once the core has its own
// helper bundle (plan 009 phase C).
func openUI(route string) {
	if coreApp != nil && coreApp.remoteWS != nil && coreApp.remoteWS.uiConnected() {
		busEmit("ui-open", map[string]string{"route": route})
		return
	}
	exe, err := os.Executable()
	if err != nil {
		log.Printf("core: open ui: %v", err)
		return
	}
	var args []string
	if route != "" {
		args = append(args, "--route", route)
	}
	if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
		cmd := []string{"-n", "-a", exe[:i+len(".app")]}
		// LaunchServices starts the UI with launchd's environment, not ours.
		for _, k := range []string{"BURROW_DATA_SUFFIX", "BURROW_HTTP_PORT"} {
			if v := os.Getenv(k); v != "" {
				cmd = append(cmd, "--env", k+"="+v)
			}
		}
		if len(args) > 0 {
			cmd = append(append(cmd, "--args"), args...)
		}
		if err := exec.Command("open", cmd...).Run(); err != nil {
			log.Printf("core: open ui: %v", err)
		}
		return
	}
	if err := spawnChild(append([]string{exe}, args...), os.Environ(), nil); err != nil {
		log.Printf("core: open ui: %v", err)
	}
}

// watchUIBinary is the UI half of plan 009's update story: the core restarts
// itself onto a new binary, but this process cannot without losing the
// window, so it only tells the frontend (update:installed → the "Update
// ready · Restart" banner, whose Restart is RelaunchApp).
func watchUIBinary() {
	started := exeFingerprint()
	if started == "" {
		return
	}
	for range time.Tick(30 * time.Second) {
		if fp := exeFingerprint(); fp != "" && fp != started {
			desktopEmit("update:installed", bundleVersion())
			return
		}
	}
}

// bundleVersion reads CFBundleShortVersionString of the bundle this binary
// is in now — the new one after an update. "" outside a bundle.
func bundleVersion() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	i := strings.Index(exe, ".app/Contents/MacOS/")
	if i < 0 {
		return ""
	}
	out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :CFBundleShortVersionString", exe[:i]+".app/Contents/Info.plist").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// uiStartRoute is the --route a core passed when it started this UI.
func uiStartRoute() string {
	for i, a := range os.Args {
		if a == "--route" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}

// uiLocalEndpoint redeems control.token for a ticket at the running core.
func uiLocalEndpoint() (LocalEndpointInfo, error) {
	var info LocalEndpointInfo
	dataDir, err := appDataDir()
	if err != nil {
		return info, err
	}
	port, err := os.ReadFile(filepath.Join(dataDir, "hook.port"))
	if err != nil {
		return info, err
	}
	token, err := os.ReadFile(filepath.Join(dataDir, "control.token"))
	if err != nil {
		return info, err
	}
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%s/v2/local-ticket", strings.TrimSpace(string(port))), nil)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("local-ticket: %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return info, err
	}
	if info.WSURL == "" {
		return info, fmt.Errorf("local-ticket: core not ready")
	}
	return info, nil
}

var ensureCoreMu sync.Mutex

// ensureCore starts `Burrow --core` unless one answers, then waits for it.
// Serialized: every window asks for a ticket on (re)connect at once.
func ensureCore() (LocalEndpointInfo, error) {
	ensureCoreMu.Lock()
	defer ensureCoreMu.Unlock()
	if info, err := uiLocalEndpoint(); err == nil {
		return info, nil
	}
	dataDir, err := appDataDir()
	if err != nil {
		return LocalEndpointInfo{}, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil { // first run: the launcher writes its plist there
		return LocalEndpointInfo{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return LocalEndpointInfo{}, err
	}
	// Never delete hook.port here: if the core is alive and only slow, the CLI
	// and every hook would lose it. A new core rewrites it.
	log.Printf("ui: starting core")
	if err := spawnDetached("core", []string{exe, "--core"}, append(os.Environ(), coreDetachedEnv+"=1"), filepath.Join(dataDir, "core.log"), filepath.Join(dataDir, "hook.port")); err != nil {
		return LocalEndpointInfo{}, err
	}
	var last error
	for i := 0; i < 150; i++ { // ≤ 15 s: a cold start migrates SQLite, starts the daemon
		time.Sleep(100 * time.Millisecond)
		info, err := uiLocalEndpoint()
		if err == nil {
			return info, nil
		}
		last = err
	}
	return LocalEndpointInfo{}, fmt.Errorf("core did not come up: %w", last)
}
