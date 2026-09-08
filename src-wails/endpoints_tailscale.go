package main

// tailscaleProvider turns the local tailnet node into endpoint candidates.
// It takes a status function rather than the App so it stays testable without
// a running app or a real tailnet.
type tailscaleProvider struct{ status func() TailscaleStatus }

func newTailscaleProvider(status func() TailscaleStatus) EndpointProvider {
	return tailscaleProvider{status: status}
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
	return []AdvertisedEndpoint{{
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
}
