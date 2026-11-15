package main

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Over-the-air install of the iOS app (ios/, `just ios-native`): a development-signed
// IPA dropped in <app-data>/ios/Burrow.ipa is offered at /ios as an
// itms-services page. iOS only installs from HTTPS with a trusted cert, which
// `tailscale serve` already gives this listener, so the page works from
// anywhere on the tailnet with no second server.
//
// Open like the PWA shell: the IPA is no credential — it installs only on
// devices in the team's provisioning profile and then has to pair.
const iosBundleID = "com.agenticide.burrow.ios"

func iosDir() string {
	d, err := appDataDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "ios")
}

func (s *HTTPServer) handleIOS(w http.ResponseWriter, r *http.Request) {
	dir := iosDir()
	ipa := filepath.Join(dir, "Burrow.ipa")
	// The manifest has to name absolute URLs; the host the phone used is the
	// one that works for it (whatever ts.net name / port serve exposes). A
	// `tailscale serve` path mount (/burrow → here) strips its prefix, so the
	// page passes it back as ?p= (it sees its own URL; we don't).
	base := "https://" + r.Host + iosPrefix(r.URL.Query().Get("p")) + "/ios"
	switch strings.TrimPrefix(r.URL.Path, "/ios") {
	case "", "/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := os.Stat(ipa); err != nil {
			fmt.Fprint(w, iosPage("", "No build yet — run <code>just ios-native</code> on the Mac."))
			return
		}
		fmt.Fprint(w, iosPage("ready", ""))
	case "/manifest.plist":
		ver, _ := os.ReadFile(filepath.Join(dir, "version"))
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, iosManifest(base+"/Burrow.ipa", strings.TrimSpace(string(ver))))
	case "/Burrow.ipa":
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, ipa)
	default:
		http.NotFound(w, r)
	}
}

// iosPrefix accepts only a plain path prefix like "/burrow" — it ends up in
// the manifest's IPA URL, so no scheme, host or traversal gets through.
func iosPrefix(p string) string {
	p = strings.TrimRight(p, "/")
	if p == "" || p[0] != '/' || strings.HasPrefix(p, "//") || strings.ContainsAny(p, ":?#\\\"'<> ") || strings.Contains(p, "..") {
		return ""
	}
	return p
}

func iosManifest(ipaURL, version string) string {
	if version == "" {
		version = "1"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>items</key><array><dict>
<key>assets</key><array><dict><key>kind</key><string>software-package</string><key>url</key><string>` + html.EscapeString(ipaURL) + `</string></dict></array>
<key>metadata</key><dict><key>bundle-identifier</key><string>` + iosBundleID + `</string><key>bundle-version</key><string>` + html.EscapeString(version) + `</string><key>kind</key><string>software</string><key>title</key><string>Burrow</string></dict>
</dict></array></dict></plist>
`
}

func iosPage(link, note string) string {
	btn := `<p>` + note + `</p>`
	if link != "" {
		// Built from the page's own URL: behind a serve path mount only the
		// browser knows the public prefix.
		btn = `<a class="btn" id="go">Install on this iPhone</a>
<p class="m">Nothing happens? Paste this into Safari's address bar:</p><input id="u" readonly onfocus="this.select()">
<script>var pre=location.pathname.replace(/\/ios\/?$/,""),m=location.origin+pre+"/ios/manifest.plist?p="+encodeURIComponent(pre),
l="itms-services://?action=download-manifest&url="+encodeURIComponent(m);document.getElementById("go").href=l;document.getElementById("u").value=l;</script>`
	}
	return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Burrow for iOS</title>
<style>body{font:17px/1.5 -apple-system,system-ui,sans-serif;margin:0;background:#111;color:#eee;display:flex;justify-content:center}
main{max-width:520px;padding:32px 22px}a.btn{display:block;text-align:center;background:#4f8cff;color:#fff;font-weight:600;padding:15px;border-radius:12px;text-decoration:none;margin:22px 0}
.m{color:#999;font-size:15px}input{width:100%;padding:8px;border-radius:8px;border:0;font-size:13px}li{margin:6px 0}</style></head><body><main>
<h1>Burrow</h1>` + btn + `<ol class="m"><li>Open this page in <b>Safari</b>.</li><li>First time: Settings → Privacy &amp; Security → <b>Developer Mode</b> on (restarts).</li>
<li>Only devices registered in the team's profile can install.</li></ol></main></body></html>`
}
