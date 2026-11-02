package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
void burrowInstallAppMenuItems(void);
*/
import "C"

// onMenuCheckUpdates is what the app menu's "Check for Updates…" runs.
var onMenuCheckUpdates func()

//export burrowMenuCheckUpdates
func burrowMenuCheckUpdates() {
	if onMenuCheckUpdates != nil {
		onMenuCheckUpdates()
	}
}

func installAppMenuItems(checkUpdates func()) {
	onMenuCheckUpdates = checkUpdates
	C.burrowInstallAppMenuItems()
}
