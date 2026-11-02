package main

import (
	"bytes"
	"encoding/json"
)

// buildChannel is "" on stable, "beta" for `just build-beta` (-ldflags -X) and
// "dev" under `wails dev` (channel_dev.go). It is decided by the binary that
// SERVES the PWA, not by whoever built the mobile bundle: the bundle is the
// same embedded files in every channel, and a build-time label would be
// whatever the last `pnpm build:mobile` happened to be.
var buildChannel = ""

// channelLabels: what the installed PWA calls itself, so a beta and a stable
// one sit side by side on a home screen and can be told apart.
var channelLabels = map[string]string{"beta": "Beta", "dev": "Dev"}

// channelizeAsset rewrites the two files that carry the PWA's identity —
// mobile.html (title, icons, `data-channel` for the UI badge) and the
// manifest (name, id, icons) — to point at public/icons/<channel>/. Every
// other file, and every file on stable, passes through untouched.
func channelizeAsset(name string, body []byte, channel string) []byte {
	label, ok := channelLabels[channel]
	if !ok {
		return body
	}
	icons := []byte("./icons/" + channel + "/")
	switch name {
	case "mobile.html":
		body = bytes.ReplaceAll(body, []byte("./icons/"), icons)
		body = bytes.Replace(body, []byte(`<html lang="en">`), []byte(`<html lang="en" data-channel="`+channel+`">`), 1)
		body = bytes.Replace(body, []byte(`content="Burrow"`), []byte(`content="Burrow `+label+`"`), 1)
		body = bytes.Replace(body, []byte("<title>Burrow Remote</title>"), []byte("<title>Burrow Remote "+label+"</title>"), 1)
		return body
	case "manifest.webmanifest":
		var m map[string]any
		if json.Unmarshal(body, &m) != nil {
			return body
		}
		m["name"] = "Burrow Remote " + label
		m["short_name"] = "Burrow " + label
		// A distinct id makes it a separate installable app, not an update of
		// the stable one (which keeps the default id = start_url).
		m["id"] = "./?channel=" + channel
		if list, ok := m["icons"].([]any); ok {
			for _, it := range list {
				if icon, ok := it.(map[string]any); ok {
					if src, ok := icon["src"].(string); ok {
						icon["src"] = string(bytes.Replace([]byte(src), []byte("./icons/"), icons, 1))
					}
				}
			}
		}
		out, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return body
		}
		return out
	}
	return body
}
