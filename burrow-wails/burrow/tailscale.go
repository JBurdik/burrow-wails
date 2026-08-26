package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// TailscaleServe/TailscaleServeStop shell out to the `tailscale` CLI's own
// `serve` subcommand to expose the local HTTP server on the tailnet — a
// thin wrapper, not a port of http_server/tailscale.rs's embedded-tsnet
// approach (that needs the tsnet Go library and its own auth key flow,
// out of scope for this pass).
func (a *App) TailscaleServe(port int) (string, error) {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return "", fmt.Errorf("tailscale CLI not found on PATH")
	}
	out, err := exec.Command("tailscale", "serve", "--bg", strconv.Itoa(port)).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tailscale serve: %w: %s", err, out)
	}
	return string(out), nil
}

func (a *App) TailscaleServeStop() error {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return fmt.Errorf("tailscale CLI not found on PATH")
	}
	return exec.Command("tailscale", "serve", "--https=443", "off").Run()
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
	path, err := exec.LookPath("tailscale")
	if err != nil {
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

	serveOut, err := exec.Command(path, "serve", "status", "--json").Output()
	if err == nil {
		var serveStatus map[string]any
		if json.Unmarshal(serveOut, &serveStatus) == nil && len(serveStatus) > 0 {
			status.Serving = true
			if status.DNSName != nil {
				url := "https://" + *status.DNSName + "/burrow"
				status.ServeURL = &url
			}
		}
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
