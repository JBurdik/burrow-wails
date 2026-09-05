package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// installWailsSink is the ONLY place the bus meets the Wails runtime. Keeping
// it in its own file is what lets events_test.go assert the boundary.
func installWailsSink(ctx context.Context) {
	busSubscribe(func(name string, payload any) {
		runtime.EventsEmit(ctx, name, payload)
	})
}
