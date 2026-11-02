package main

import (
	"fmt"
	"net/netip"
)

// AdvertisedEndpoint is a server-authored candidate route to this environment.
// It is a HINT, not proof: only a successful connection from the client's own
// network decides whether the route works.
type AdvertisedEndpoint struct {
	Kind                  string `json:"kind"` // loopback | tailscale-magicdns | ssh-forward | ...
	HTTPBase              string `json:"http_base"`
	WSBase                string `json:"ws_base"`
	Reachability          string `json:"reachability"` // loopback | lan | private | public | tunnel
	HostedHTTPSCompatible bool   `json:"hosted_https_compatible"`
	Default               bool   `json:"default"`
	Available             bool   `json:"available"`
}

// EndpointProvider contributes candidate endpoints. Providers live OUTSIDE the
// core environment model on purpose: Tailscale is the first one, not a branch
// in the model, so a future tunnel plugs into the same shape.
type EndpointProvider interface {
	Name() string
	// Endpoints is best-effort. A provider that cannot answer returns an empty
	// slice rather than an error — a broken provider must not break the list.
	Endpoints() []AdvertisedEndpoint
}

type loopbackProvider struct{ port int }

func newLoopbackProvider(port int) EndpointProvider { return loopbackProvider{port: port} }

func (p loopbackProvider) Name() string { return "loopback" }

func (p loopbackProvider) Endpoints() []AdvertisedEndpoint {
	if p.port == 0 {
		return nil
	}
	authority := fmt.Sprintf("127.0.0.1:%d", p.port)
	return []AdvertisedEndpoint{{
		Kind:         "loopback",
		HTTPBase:     "http://" + authority,
		WSBase:       "ws://" + authority,
		Reachability: "loopback",
		Default:      true,
		Available:    true,
	}}
}

// selectEndpoint implements t3code's order verbatim (docs/architecture/remote.md):
// user preference by KIND (never by URL — a tailnet address changes), then
// hosted-HTTPS compatible, then explicitly default, then non-loopback, and
// loopback only for a client on this same machine.
func selectEndpoint(eps []AdvertisedEndpoint, preferredKind string, sameMachine bool) *AdvertisedEndpoint {
	usable := make([]AdvertisedEndpoint, 0, len(eps))
	for _, e := range eps {
		if !e.Available {
			continue
		}
		if e.Reachability == "loopback" && !sameMachine {
			continue
		}
		usable = append(usable, e)
	}

	pick := func(match func(AdvertisedEndpoint) bool) *AdvertisedEndpoint {
		for i := range usable {
			if match(usable[i]) {
				return &usable[i]
			}
		}
		return nil
	}

	if preferredKind != "" {
		if e := pick(func(e AdvertisedEndpoint) bool { return e.Kind == preferredKind }); e != nil {
			return e
		}
	}
	if e := pick(func(e AdvertisedEndpoint) bool { return e.HostedHTTPSCompatible }); e != nil {
		return e
	}
	if e := pick(func(e AdvertisedEndpoint) bool { return e.Default }); e != nil {
		return e
	}
	if e := pick(func(e AdvertisedEndpoint) bool { return e.Reachability != "loopback" }); e != nil {
		return e
	}
	return pick(func(AdvertisedEndpoint) bool { return true })
}

// Address rules ported from t3code (DirectEndpoints.ts isAdvertisableAddress,
// hostClassification.ts isTailnetHost). Pairing credentials travel over these
// URLs, so a public address is never advertised, and a hostname is never
// trusted (it can resolve differently for the phone).
var (
	tailnetV4 = netip.MustParsePrefix("100.64.0.0/10") // CGNAT range Tailscale assigns from
	tailnetV6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// isTailnetAddr reports whether s is a Tailscale node address. 100.64/10 is
// shared address space (carrier NAT, WARP use it too), so a caller that must
// be sure also needs Tailscale itself to report the address.
func isTailnetAddr(s string) bool {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return false
	}
	a = a.Unmap()
	return tailnetV4.Contains(a) || tailnetV6.Contains(a)
}

// isAdvertisableAddr is t3code's rule: an IPv4 literal that is private (RFC
// 1918 or the tailnet range), never loopback, link-local or public.
func isAdvertisableAddr(s string) bool {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return false
	}
	a = a.Unmap()
	if !a.Is4() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	return a.IsPrivate() || tailnetV4.Contains(a)
}

// collectEndpoints asks every provider in order. A provider that returns
// nothing is normal, not an error.
func collectEndpoints(providers []EndpointProvider) []AdvertisedEndpoint {
	out := make([]AdvertisedEndpoint, 0, len(providers))
	for _, p := range providers {
		out = append(out, p.Endpoints()...)
	}
	return out
}

// RemoteEndpoints is a DESKTOP binding, deliberately not an HTTP route: the
// list of ways to reach this machine is recon information and nobody needs it
// before they are connected. Settings renders it; pairing (phase 5) uses it.
func (a *App) RemoteEndpoints() []AdvertisedEndpoint {
	return collectEndpoints(a.endpointProviders)
}

// RemotePairingEndpoint is the address a phone should pair with: the
// endpoint list run through selectEndpoint as seen from ANOTHER machine, so
// loopback never wins. nil when there is no usable route yet (Tailscale off,
// not serving) — Settings then shows the code without a QR.
func (a *App) RemotePairingEndpoint() *AdvertisedEndpoint {
	return selectEndpoint(a.RemoteEndpoints(), "", false)
}
