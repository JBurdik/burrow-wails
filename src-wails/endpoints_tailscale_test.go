package main

import "testing"

func strp(s string) *string { return &s }

func TestTailscaleProviderAdvertisesMagicDNS(t *testing.T) {
	p := newTailscaleProvider(func() TailscaleStatus {
		return TailscaleStatus{Installed: true, LoggedIn: true, DNSName: strp("mac-mini.tail1234.ts.net"), Serving: true}
	})
	eps := p.Endpoints()
	if len(eps) != 1 {
		t.Fatalf("want 1 endpoint, got %d", len(eps))
	}
	e := eps[0]
	if e.Kind != "tailscale-magicdns" {
		t.Fatalf("bad kind: %q", e.Kind)
	}
	if e.WSBase != "wss://mac-mini.tail1234.ts.net/burrow" {
		t.Fatalf("bad ws base: %q", e.WSBase)
	}
	if !e.HostedHTTPSCompatible {
		t.Fatal("MagicDNS HTTPS must be hosted-compatible")
	}
	if e.Reachability != "private" {
		t.Fatalf("bad reachability: %q", e.Reachability)
	}
	if !e.Available {
		t.Fatal("serving node must be available")
	}
}

func TestTailscaleProviderUnavailableWhenNotServing(t *testing.T) {
	p := newTailscaleProvider(func() TailscaleStatus {
		return TailscaleStatus{Installed: true, LoggedIn: true, DNSName: strp("mac-mini.tail1234.ts.net"), Serving: false}
	})
	eps := p.Endpoints()
	if len(eps) != 1 {
		t.Fatalf("want the endpoint listed but unavailable, got %d", len(eps))
	}
	if eps[0].Available {
		t.Fatal("a node that is not serving must not be advertised as available")
	}
}

func TestTailscaleProviderSilentWhenLoggedOut(t *testing.T) {
	p := newTailscaleProvider(func() TailscaleStatus { return TailscaleStatus{Installed: true} })
	if got := p.Endpoints(); len(got) != 0 {
		t.Fatalf("logged-out node advertised %+v", got)
	}
}
