package main

import "testing"

func strp(s string) *string { return &s }

func TestTailscaleProviderAdvertisesMagicDNS(t *testing.T) {
	p := newTailscaleProvider(func() TailscaleStatus {
		return TailscaleStatus{Installed: true, LoggedIn: true, DNSName: strp("mac-mini.tail1234.ts.net"), Serving: true}
	}, 0)
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
	}, 0)
	eps := p.Endpoints()
	if len(eps) != 1 {
		t.Fatalf("want the endpoint listed but unavailable, got %d", len(eps))
	}
	if eps[0].Available {
		t.Fatal("a node that is not serving must not be advertised as available")
	}
}

func TestTailscaleProviderSilentWhenLoggedOut(t *testing.T) {
	p := newTailscaleProvider(func() TailscaleStatus { return TailscaleStatus{Installed: true} }, 0)
	if got := p.Endpoints(); len(got) != 0 {
		t.Fatalf("logged-out node advertised %+v", got)
	}
}

func TestTailscaleProviderListsTailnetIPUnavailable(t *testing.T) {
	p := newTailscaleProvider(func() TailscaleStatus {
		return TailscaleStatus{Installed: true, LoggedIn: true, DNSName: strp("mac-mini.tail1234.ts.net"), Serving: true, IP: strp("100.101.102.103")}
	}, 37892)
	eps := p.Endpoints()
	if len(eps) != 2 || eps[1].Kind != "tailnet-ip" || eps[1].HTTPBase != "http://100.101.102.103:37892" {
		t.Fatalf("want MagicDNS then tailnet IP, got %+v", eps)
	}
	if eps[1].Available {
		t.Fatal("the listener is loopback-only, so the tailnet IP cannot be available")
	}
	if got := selectEndpoint(eps, "", false); got == nil || got.Kind != "tailscale-magicdns" {
		t.Fatalf("pairing must prefer MagicDNS HTTPS, got %+v", got)
	}
}

func TestTailscaleProviderNeverAdvertisesPublicIP(t *testing.T) {
	p := newTailscaleProvider(func() TailscaleStatus {
		return TailscaleStatus{Installed: true, LoggedIn: true, DNSName: strp("mac-mini.tail1234.ts.net"), IP: strp("8.8.8.8")}
	}, 37892)
	for _, e := range p.Endpoints() {
		if e.Kind == "tailnet-ip" {
			t.Fatalf("public IP advertised: %+v", e)
		}
	}
}

func TestAddressClassification(t *testing.T) {
	for _, c := range []struct {
		addr                string
		tailnet, advertised bool
	}{
		{"100.64.0.1", true, true},
		{"100.127.255.254", true, true},
		{"100.128.0.1", false, false}, // just past 100.64/10: public
		{"fd7a:115c:a1e0::1", true, false},
		{"fd7a:115c:a1e1::1", false, false},
		{"::ffff:100.100.1.1", true, true},
		{"192.168.1.10", false, true},
		{"10.0.0.5", false, true},
		{"172.16.0.1", false, true},
		{"172.32.0.1", false, false},
		{"127.0.0.1", false, false},
		{"169.254.1.1", false, false},
		{"8.8.8.8", false, false},
		{"fe80::1", false, false},
		{"mac-mini.tail1234.ts.net", false, false}, // hostnames are never trusted
		{"", false, false},
	} {
		if got := isTailnetAddr(c.addr); got != c.tailnet {
			t.Errorf("isTailnetAddr(%q) = %v", c.addr, got)
		}
		if got := isAdvertisableAddr(c.addr); got != c.advertised {
			t.Errorf("isAdvertisableAddr(%q) = %v", c.addr, got)
		}
	}
}

func TestHTTPSCapability(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		raw  string
		want *bool
	}{
		{`{"https":[],"funnel":[]}`, &yes},
		{`{"funnel":[]}`, &no},
		{`{}`, &no},
		{`null`, nil},
		{``, nil},
		{`[]`, nil},
	} {
		got := httpsCapability([]byte(c.raw))
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("httpsCapability(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}
