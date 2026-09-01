package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// tailscaleBin resolves the CLI. exec.LookPath alone is not enough: a bundle
// launched from Finder inherits launchd's PATH (/usr/bin:/bin:/usr/sbin:/sbin),
// which excludes the /usr/local/bin shim the macOS Tailscale app installs — so
// the toggle reported "not installed" and sat disabled on a working tailnet.
func tailscaleBin() string {
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p
	}
	for _, c := range []string{
		"/usr/local/bin/tailscale",
		"/opt/homebrew/bin/tailscale",
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
	} {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		c := filepath.Join(home, "Applications/Tailscale.app/Contents/MacOS/Tailscale")
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// TailscaleServe/TailscaleServeStop shell out to the `tailscale` CLI's own
// `serve` subcommand to expose the local HTTP server on the tailnet — a
// thin wrapper, not a port of http_server/tailscale.rs's embedded-tsnet
// approach (that needs the tsnet Go library and its own auth key flow,
// out of scope for this pass).
// tailscaleServePath is the sub-path Burrow publishes on. Mounting at "/"
// would clobber whatever else the user already serves on this tailnet
// node, which is exactly what the Settings copy promises not to do.
const tailscaleServePath = "/burrow"

func (a *App) TailscaleServe(port int) (string, error) {
	bin := tailscaleBin()
	if bin == "" {
		return "", fmt.Errorf("tailscale CLI not found on PATH")
	}
	target := fmt.Sprintf("http://127.0.0.1:%d", port)
	out, err := exec.Command(bin, "serve", "--bg",
		"--set-path="+tailscaleServePath, target).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tailscale serve: %w: %s", err, out)
	}
	return string(out), nil
}

func (a *App) TailscaleServeStop() error {
	bin := tailscaleBin()
	if bin == "" {
		return fmt.Errorf("tailscale CLI not found on PATH")
	}
	// Scoped to our own path — a bare `serve off` would tear down every
	// other handler on this node too.
	return exec.Command(bin, "serve", "--https=443",
		"--set-path="+tailscaleServePath, "off").Run()
}

// TailscaleStatus mirrors Settings.vue's local TailscaleStatus interface.
type TailscaleStatus struct {
	Installed bool    `json:"installed"`
	LoggedIn  bool    `json:"logged_in"`
	DNSName   *string `json:"dns_name"`
	Serving   bool    `json:"serving"`
	ServeURL  *string `json:"serve_url"`
}

func (a *App) GetTailscaleStatus() TailscaleStatus {
	path := tailscaleBin()
	if path == "" {
		return TailscaleStatus{}
	}
	status := TailscaleStatus{Installed: true}

	out, err := exec.Command(path, "status", "--json").Output()
	if err != nil {
		return status
	}
	var ts struct {
		Self struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
		BackendState string `json:"BackendState"`
	}
	if json.Unmarshal(out, &ts) == nil {
		status.LoggedIn = ts.BackendState == "Running"
		if ts.Self.DNSName != "" {
			name := strings.TrimSuffix(ts.Self.DNSName, ".")
			status.DNSName = &name
		}
	}

	// "Serving" must mean *our* path points at *our* port. Checking only
	// that some serve config exists reported success while /burrow was
	// still proxying to a dead port from an older build.
	serveOut, err := exec.Command(path, "serve", "status", "--json").Output()
	if err == nil {
		var serveStatus struct {
			Web map[string]struct {
				Handlers map[string]struct {
					Proxy string `json:"Proxy"`
				} `json:"Handlers"`
			} `json:"Web"`
		}
		want := fmt.Sprintf("http://127.0.0.1:%d", httpServerPort)
		if json.Unmarshal(serveOut, &serveStatus) == nil {
			for _, host := range serveStatus.Web {
				if h, ok := host.Handlers[tailscaleServePath]; ok && h.Proxy == want {
					status.Serving = true
				}
			}
		}
	}
	if status.Serving && status.DNSName != nil {
		// Trailing slash matters: the bundle loads its assets relatively,
		// so from ".../burrow" they would resolve against "/" and 404.
		url := "https://" + *status.DNSName + tailscaleServePath + "/"
		status.ServeURL = &url
	}
	return status
}

func (a *App) SetTailscaleServe(enabled bool, port int) (TailscaleStatus, error) {
	var err error
	if enabled {
		_, err = a.TailscaleServe(port)
	} else {
		err = a.TailscaleServeStop()
	}
	return a.GetTailscaleStatus(), err
}
