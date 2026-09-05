package main

import "testing"

func ep(kind string, hosted, def bool) AdvertisedEndpoint {
	reach := "private"
	if kind == "loopback" {
		reach = "loopback"
	}
	return AdvertisedEndpoint{Kind: kind, Reachability: reach, HostedHTTPSCompatible: hosted, Default: def, Available: true}
}

func TestSelectEndpointPrefersUserKind(t *testing.T) {
	eps := []AdvertisedEndpoint{ep("tailscale-magicdns", true, false), ep("loopback", false, true)}
	got := selectEndpoint(eps, "loopback", true)
	if got == nil || got.Kind != "loopback" {
		t.Fatalf("user preference ignored: %+v", got)
	}
}

func TestSelectEndpointIgnoresUnavailablePreference(t *testing.T) {
	unavailable := ep("tailscale-magicdns", true, false)
	unavailable.Available = false
	eps := []AdvertisedEndpoint{unavailable, ep("loopback", false, true)}
	got := selectEndpoint(eps, "tailscale-magicdns", true)
	if got == nil || got.Kind != "loopback" {
		t.Fatalf("fell back wrong: %+v", got)
	}
}

func TestSelectEndpointOrder(t *testing.T) {
	hosted := ep("tailscale-magicdns", true, false)
	dflt := ep("ssh-forward", false, true)
	plain := ep("lan", false, false)
	loop := ep("loopback", false, false)

	if got := selectEndpoint([]AdvertisedEndpoint{loop, plain, dflt, hosted}, "", false); got.Kind != "tailscale-magicdns" {
		t.Fatalf("hosted-https must win: %+v", got)
	}
	if got := selectEndpoint([]AdvertisedEndpoint{loop, plain, dflt}, "", false); got.Kind != "ssh-forward" {
		t.Fatalf("default must win over plain: %+v", got)
	}
	if got := selectEndpoint([]AdvertisedEndpoint{loop, plain}, "", false); got.Kind != "lan" {
		t.Fatalf("non-loopback must win: %+v", got)
	}
}

func TestSelectEndpointLoopbackOnlySameMachine(t *testing.T) {
	eps := []AdvertisedEndpoint{ep("loopback", false, true)}
	if got := selectEndpoint(eps, "", false); got != nil {
		t.Fatalf("loopback offered to a remote client: %+v", got)
	}
	if got := selectEndpoint(eps, "", true); got == nil {
		t.Fatal("loopback withheld from a same-machine client")
	}
}

func TestLoopbackProvider(t *testing.T) {
	eps := newLoopbackProvider(4242).Endpoints()
	if len(eps) != 1 {
		t.Fatalf("want 1 endpoint, got %d", len(eps))
	}
	if eps[0].WSBase != "ws://127.0.0.1:4242" {
		t.Fatalf("bad ws base: %q", eps[0].WSBase)
	}
	if got := newLoopbackProvider(0).Endpoints(); len(got) != 0 {
		t.Fatalf("port 0 must advertise nothing, got %+v", got)
	}
}
