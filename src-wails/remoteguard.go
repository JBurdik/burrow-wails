package main

import (
	"encoding/json"
	"fmt"
	"net"
)

// Startup guards for the remote listener. Both are pure functions over their
// input so the invariants they enforce are testable without a tailnet, a
// network interface or a running app — spec §4 requires each to exist as a
// test, not only as a comment, because each can be broken by a silent config
// change somewhere else.

// funnelCheckHook stands in for the tailscale shell-out so the refusal path
// itself can be tested, not only the JSON parse below. Always nil in
// production. Spec §4 wants "funnel on → the server refuses to start" as a
// test, and that sentence is about setHttpEnabled, not about a parser.
var funnelCheckHook func() bool

// funnelEnabledIn reports whether `tailscale funnel` publishes anything on
// this node, given the output of `tailscale serve status --json`.
//
// It deliberately does NOT try to match the hostport against our own node or
// our own path. Funnel is granted per host:port, our serve sits on that same
// :443, so a funnel there publishes /burrow whether it was turned on for us
// or for something else sharing the node.
//
// Unparseable input counts as ENABLED. If we cannot read the config we cannot
// claim the handler is private, and the whole point of this check is that
// pairing with six digits is defensible against a tailnet and not against the
// open internet.
func funnelEnabledIn(serveStatusJSON []byte) bool {
	var st struct {
		// Measured shape: with funnel off this key is ABSENT, not false. A
		// check written against `== false` would read "off" as "on".
		AllowFunnel map[string]bool `json:"AllowFunnel"`
	}
	if err := json.Unmarshal(serveStatusJSON, &st); err != nil {
		return true
	}
	for _, on := range st.AllowFunnel {
		if on {
			return true
		}
	}
	return false
}

// assertLoopbackAddr rejects any bind address that is not loopback.
//
// The only way in from the tailnet is `tailscale serve`, which terminates real
// HTTPS and forwards to loopback. Binding a tailnet or LAN address directly is
// refused for two reasons, and the second one outlives any auth work: plain
// HTTP on a private IP is not a secure context, so a browser there gets no
// service worker — no installable PWA, and later no push.
func assertLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("remote listener address %q: %w", addr, err)
	}
	// An empty host is a wildcard bind (":37892" listens on every interface).
	// That is the mistake this function exists to catch, not a default.
	if host == "" {
		return fmt.Errorf("remote listener address %q binds every interface; loopback only", addr)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("remote listener host %q is not an IP or localhost", host)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("remote listener host %q is not loopback; the tailnet reaches us through `tailscale serve`", host)
	}
	return nil
}
