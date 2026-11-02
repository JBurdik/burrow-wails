//go:build darwin && !headless

package main

/*
#cgo LDFLAGS: -framework Carbon
#include <Carbon/Carbon.h>

extern void goTrayHotkey(void);

static EventHotKeyRef trayHotkeyRef = NULL;
static int trayHandlerInstalled = 0;

static OSStatus trayHotkeyHandler(EventHandlerCallRef next, EventRef ev, void *data) {
	goTrayHotkey();
	return noErr;
}

// Carbon's RegisterEventHotKey needs no Accessibility permission (an event
// tap would) and is still the API for a global shortcut on macOS. Replaces
// the previous one; key 0 + mods 0 = just unregister.
static int setTrayHotkey(UInt32 key, UInt32 mods) {
	if (!trayHandlerInstalled) {
		EventTypeSpec spec = { kEventClassKeyboard, kEventHotKeyPressed };
		InstallApplicationEventHandler(&trayHotkeyHandler, 1, &spec, NULL, NULL);
		trayHandlerInstalled = 1;
	}
	if (trayHotkeyRef) {
		UnregisterEventHotKey(trayHotkeyRef);
		trayHotkeyRef = NULL;
	}
	if (key == 0 && mods == 0) return 0;
	EventHotKeyID id = { 'BRRW', 1 };
	return (int)RegisterEventHotKey(key, mods, id, GetApplicationEventTarget(), 0, &trayHotkeyRef);
}
*/
import "C"

import (
	"fmt"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// setTrayHotkey (re)registers the tray panel's global shortcut ("" = none).
// Off the main thread only: InvokeSync from it would deadlock.
func setTrayHotkey(spec string) error {
	var key, mods uint32
	if spec != "" {
		var err error
		if key, mods, err = parseHotkey(spec); err != nil {
			return err
		}
	}
	var st C.int
	application.InvokeSync(func() { st = C.setTrayHotkey(C.UInt32(key), C.UInt32(mods)) })
	if st != 0 {
		return fmt.Errorf("%s is taken by another app or the system (OSStatus %d)", spec, int(st))
	}
	return nil
}

func registerTrayHotkey(a *App) {
	if err := setTrayHotkey(a.trayHotkeySpec()); err != nil {
		log.Printf("tray hotkey: %v", err)
	}
}

//export goTrayHotkey
func goTrayHotkey() {
	go trayHotkey() // off the main thread: Show/Focus dispatch back onto it
}

func trayHotkey() {
	if trayWin == nil {
		return
	}
	if trayWin.IsVisible() {
		trayWin.Hide()
		return
	}
	showTrayPanel(!trayPinned.Load())
	desktopEmit("menu-tray-compose")
}
