package main

import "fmt"

// tailscaleProvider turns the local tailnet node into endpoint candidates.
// It takes a status function rather than the App so it stays testable without
// a running app or a real tailnet.
type tailscaleProvider struct {
	status func() TailscaleStatus
	port   int
}

func newTailscaleProvider(status func() TailscaleStatus, port int) EndpointProvider {
	return tailscaleProvider{status: status, port: port}
}

func (p tailscaleProvider) Name() string { return "tailscale" }

func (p tailscaleProvider) Endpoints() []AdvertisedEndpoint {
	s := p.status()
	if !s.Installed || !s.LoggedIn || s.DNSName == nil || *s.DNSName == "" {
		return nil
	}
	// The path must match what TailscaleServe mounts; serving at "/" would
	// clobber whatever else this node already publishes.
	authority := *s.DNSName + tailscaleServePath
	eps := []AdvertisedEndpoint{{
		Kind:     "tailscale-magicdns",
		HTTPBase: "https://" + authority,
		WSBase:   "wss://" + authority,
		// Private, not public: reachable inside the tailnet only. Funnel is
		// refused outright (see phase 5), so this never becomes "public".
		Reachability: "private",
		// MagicDNS terminates real HTTPS, which is what makes the PWA a secure
		// context — service worker, install prompt and (later) push depend on it.
		HostedHTTPSCompatible: true,
		// Listed but unavailable when the node is not serving our path, so the
		// UI can say "Tailscale is there, turn sharing on" instead of hiding it.
		Available: s.Serving,
	}}
	// The node's tailnet IP, second choice. Only an address Tailscale reports
	// AND that sits in the tailnet range — never a public one. Unavailable
	// for now: the listener binds loopback only (assertLoopbackAddr), so
	// nothing answers on this IP; it is listed so the list says why.
	// ponytail: flip Available once a tailnet-interface listener exists.
	if s.IP != nil && isTailnetAddr(*s.IP) && isAdvertisableAddr(*s.IP) && p.port != 0 {
		authority := fmt.Sprintf("%s:%d", *s.IP, p.port)
		eps = append(eps, AdvertisedEndpoint{
			Kind:         "tailnet-ip",
			HTTPBase:     "http://" + authority,
			WSBase:       "ws://" + authority,
			Reachability: "private",
		})
	}
	return eps
}
