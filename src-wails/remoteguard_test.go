package main

import (
	"strings"
	"testing"
)

// The exact shape `tailscale serve status --json` printed on the dev machine
// with funnel off. Kept verbatim rather than minimized: the point of the test
// is that the real output has no AllowFunnel key at all.
const serveStatusFunnelOff = `{
  "TCP": {"443": {"HTTPS": true}},
  "Web": {
    "mac-mini.tailnet.ts.net:443": {
      "Handlers": {
        "/": {"Proxy": "http://127.0.0.1:3773"},
        "/burrow": {"Proxy": "http://127.0.0.1:37892"}
      }
    }
  }
}`

const serveStatusFunnelOn = `{
  "AllowFunnel": {"mac-mini.tailnet.ts.net:443": true},
  "TCP": {"443": {"HTTPS": true}},
  "Web": {
    "mac-mini.tailnet.ts.net:443": {
      "Handlers": {"/burrow": {"Proxy": "http://127.0.0.1:37892"}}
    }
  }
}`

func TestFunnelOffInRealServeStatus(t *testing.T) {
	// With funnel off the AllowFunnel key is ABSENT, not false. A checker
	// written against `== false` would read "off" as "on" and refuse to start
	// for every user who has serve configured.
	if funnelEnabledIn([]byte(serveStatusFunnelOff)) {
		t.Fatal("funnel reported on when the key is absent")
	}
}

func TestFunnelOnIsDetected(t *testing.T) {
	if !funnelEnabledIn([]byte(serveStatusFunnelOn)) {
		t.Fatal("funnel on was not detected")
	}
}

func TestFunnelOnForAnotherPathStillCounts(t *testing.T) {
	// Funnel is granted per host:port and our serve sits on that same :443,
	// so a funnel turned on for something else on this node publishes our
	// path too. Matching on our own path would miss it.
	const on = `{"AllowFunnel":{"h:443":true},"Web":{"h:443":{"Handlers":{"/other":{"Proxy":"http://127.0.0.1:9999"}}}}}`
	if !funnelEnabledIn([]byte(on)) {
		t.Fatal("a funnel on this node's 443 must count regardless of path")
	}
}

func TestUnreadableServeStatusFailsClosed(t *testing.T) {
	// If we cannot read the config we cannot claim the handler is private.
	if !funnelEnabledIn([]byte("not json")) {
		t.Fatal("unreadable serve config must fail closed")
	}
}

func TestAllowFunnelPresentButFalseIsOff(t *testing.T) {
	const off = `{"AllowFunnel":{"h:443":false},"Web":{}}`
	if funnelEnabledIn([]byte(off)) {
		t.Fatal("an explicit false must read as off")
	}
}

func TestFunnelOnRefusesToStartTheServer(t *testing.T) {
	// Spec §4 invariant 2 is about the START, not about the parser: fail
	// closed, with a message the user can act on, and no listener.
	funnelCheckHook = func() bool { return true }
	t.Cleanup(func() { funnelCheckHook = nil })

	a := &App{}
	err := a.setHttpEnabled(true)
	if err == nil {
		t.Fatal("remote access started with funnel on")
	}
	if a.httpSrv != nil || a.httpSrvRunning {
		t.Fatalf("a refused start left state behind: srv=%v running=%v", a.httpSrv != nil, a.httpSrvRunning)
	}
	if !strings.Contains(err.Error(), "funnel") {
		t.Fatalf("the error must name funnel so the user can fix it: %v", err)
	}
}

func TestOnlyLoopbackMayBeBound(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:37892", "localhost:37892", "[::1]:37892"} {
		if err := assertLoopbackAddr(ok); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
	// A tailnet IP, a LAN IP and a wildcard are the same mistake: the only
	// way in from the tailnet is `tailscale serve`, which terminates HTTPS
	// and forwards to loopback. Plain HTTP on a private IP is also not a
	// secure context, so a PWA served there gets no service worker.
	for _, bad := range []string{"100.64.0.1:37892", "0.0.0.0:37892", ":37892", "192.168.1.5:37892", "[::]:37892"} {
		if err := assertLoopbackAddr(bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}
