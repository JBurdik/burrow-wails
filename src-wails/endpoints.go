package main

import "fmt"

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
