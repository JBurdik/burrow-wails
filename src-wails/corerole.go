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

// Plan 009: on dev and beta the backend (App) runs as its own process —
// `Burrow --core` — and the window process is only a client of its /v2/ws,
// like the phone. Stable keeps the in-process App until it switches over.

// externalCore: every channel but stable ("") splits the backend out.
func externalCore() bool { return buildChannel != "" }

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
	dataDir, err := appDataDir()
	if err != nil {
		log.Fatalf("core: app data dir: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("core: %v", err)
	}
	// One core per data dir: a UI and a CLI racing to start one must not get
	// two backends writing the same SQLite and config.json.
	lock, err := os.OpenFile(filepath.Join(dataDir, "core.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		log.Fatalf("core: lock: %v", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		log.Printf("core: another core owns %s, exiting", dataDir)
		return
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
	wapp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			if ok, err := n.RequestNotificationAuthorization(); !ok {
				log.Printf("core: notifications not authorized (%v)", err)
			}
		}()
	})
	n.OnNotificationResponse(func(r notifications.NotificationResult) {
		if r.Error != nil {
			return
		}
		url, _ := r.Response.UserInfo["url"].(string)
		openUI(strings.TrimPrefix(url, "#"))
	})
	nativeNotify = func(title, body, url, tag string) {
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
}

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
	if err := spawnDetached("core", []string{exe, "--core"}, os.Environ(), filepath.Join(dataDir, "core.log"), filepath.Join(dataDir, "hook.port")); err != nil {
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
