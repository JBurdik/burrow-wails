package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAPNsJWTVerifies(t *testing.T) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := time.Unix(1_700_000_000, 0)
	tok, err := apnsJWT(k, "KEY123", "TEAM456", now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d parts", len(parts))
	}
	var hdr map[string]string
	var claims map[string]any
	h, _ := b64.DecodeString(parts[0])
	c, _ := b64.DecodeString(parts[1])
	_ = json.Unmarshal(h, &hdr)
	_ = json.Unmarshal(c, &claims)
	if hdr["alg"] != "ES256" || hdr["kid"] != "KEY123" || claims["iss"] != "TEAM456" || claims["iat"] != float64(now.Unix()) {
		t.Fatalf("header %v claims %v", hdr, claims)
	}
	sig, _ := b64.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(&k.PublicKey, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("signature does not verify")
	}
}

func TestAPNsPayload(t *testing.T) {
	var p struct {
		Aps struct {
			Alert    map[string]string `json:"alert"`
			ThreadID string            `json:"thread-id"`
		} `json:"aps"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(apnsPayload(pushMsg{Title: "Fix bug", Body: "Done", URL: "#/ws/3?chat=7", Tag: "chat:7"}), &p); err != nil {
		t.Fatal(err)
	}
	if p.Aps.Alert["title"] != "Fix bug" || p.Aps.Alert["body"] != "Done" || p.Aps.ThreadID != "chat:7" || p.URL != "#/ws/3?chat=7" {
		t.Fatalf("payload %+v", p)
	}
}

// Every dead-token answer forgets the row; a transient error and a 200 keep it.
func TestAPNsDeadTokensAreForgotten(t *testing.T) {
	answers := map[string]struct {
		code   int
		reason string
	}{
		"aa": {http.StatusGone, "Unregistered"},
		"bb": {http.StatusBadRequest, "BadDeviceToken"},
		"cc": {http.StatusOK, ""},
		"dd": {http.StatusTooManyRequests, "TooManyRequests"},
	}
	var sandboxHits int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("authorization"), "bearer ") || r.Header.Get("apns-topic") != "com.example.app" ||
			r.Header.Get("apns-push-type") != "alert" || r.Header.Get("apns-collapse-id") != "chat-1" {
			t.Errorf("headers %v", r.Header)
		}
		if strings.HasPrefix(r.URL.Path, "/sandbox/") {
			sandboxHits++
		}
		tok := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		ans := answers[tok[:2]]
		w.WriteHeader(ans.code)
		if ans.reason != "" {
			_ = json.NewEncoder(w).Encode(map[string]string{"reason": ans.reason})
		}
	}))
	defer srv.Close()

	a := newTestApp(t)
	a.push = newWebPusher(filepath.Join(t.TempDir(), "vapid.pem"))
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	a.push.apns = &apnsSender{cfg: apnsConfig{KeyID: "K", TeamID: "T", Topic: "com.example.app"}, key: k,
		client: srv.Client(), prodURL: srv.URL + "/prod", sandboxURL: srv.URL + "/sandbox"}
	_, dev, _ := a.pairDevice("phone", "phone")
	for p := range answers {
		if err := a.PushSubscribeAPNs(dev.ID, p+strings.Repeat("0", 62), p == "cc"); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.PushSubscribeAPNs("", "ee"+strings.Repeat("0", 62), false); err == nil {
		t.Fatal("the desktop (no device) must not subscribe")
	}
	if err := a.PushSubscribeAPNs(dev.ID, "not-hex", false); err == nil {
		t.Fatal("a non-hex token must be refused")
	}

	a.pushToAll(pushMsg{Title: "t", Tag: "chat:1"}, "chat-1")
	if sandboxHits != 1 {
		t.Fatalf("sandbox host hit %d times, want 1", sandboxHits)
	}
	var left []string
	rows, _ := a.db.Query(`SELECT substr(token, 1, 2) FROM apns_tokens ORDER BY token`)
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		left = append(left, s)
	}
	rows.Close()
	if strings.Join(left, ",") != "cc,dd" {
		t.Fatalf("left %v, want cc,dd", left)
	}

	devs, _ := a.RemoteDevices()
	if len(devs) != 1 || !devs[0].Push {
		t.Fatalf("device should report push: %+v", devs)
	}
	_ = a.RevokeRemoteDevice(dev.ID)
	var n int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM apns_tokens`).Scan(&n)
	if n != 0 {
		t.Fatalf("revoke left %d apns tokens", n)
	}
}
