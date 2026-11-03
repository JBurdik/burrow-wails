//go:build darwin && !headless

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework ServiceManagement -framework Foundation
#import <Foundation/Foundation.h>
#import <ServiceManagement/ServiceManagement.h>
#include <stdlib.h>

// op: 0 status, 1 register, 2 unregister. Returns SMAppServiceStatus
// (0 notRegistered, 1 enabled, 2 requiresApproval, 3 notFound), -1 before
// macOS 13, -2 on error (*err set, caller frees).
static int coreLoginItem(int op, char **err) {
	if (@available(macOS 13.0, *)) {
		SMAppService *s = [SMAppService agentServiceWithPlistName:@"core.plist"];
		NSError *e = nil;
		if (op == 1) {
			[s registerAndReturnError:&e];
		} else if (op == 2) {
			[s unregisterAndReturnError:&e];
		}
		if (e != nil) {
			*err = strdup(e.localizedDescription.UTF8String);
			return -2;
		}
		return (int)s.status;
	}
	return -1;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// coreLoginItem registers / unregisters / reads the core's login item
// (Contents/Library/LaunchAgents/core.plist, plan 009).
func coreLoginItem(op int) (string, error) {
	var cerr *C.char
	r := C.coreLoginItem(C.int(op), &cerr)
	if cerr != nil {
		defer C.free(unsafe.Pointer(cerr))
	}
	switch r {
	case 0:
		return "off", nil
	case 1:
		return "on", nil
	case 2:
		return "needs-approval", nil
	case 3:
		return "unavailable", nil // no core.plist in this bundle (dev build)
	case -1:
		return "unsupported", nil // macOS < 13
	}
	return "error", errors.New(C.GoString(cerr))
}
