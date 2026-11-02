package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func testSubscription(t *testing.T, endpoint string) pushSubscription {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	var s pushSubscription
	s.Endpoint = endpoint
	s.Keys.P256dh = b64.EncodeToString(k.PublicKey().Bytes())
	s.Keys.Auth = b64.EncodeToString(auth)
	return s
}

func pushCount(t *testing.T, a *App, deviceID string) int {
	t.Helper()
	var n int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE device_id = ?`, deviceID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPushSubscriptionCRUDAndRevokeCascade(t *testing.T) {
	a := newTestApp(t)
	_, dev, err := a.pairDevice("phone", "phone")
	if err != nil {
		t.Fatal(err)
	}
	_, other, _ := a.pairDevice("tablet", "phone")
	sub := testSubscription(t, "https://push.example.net/a")

	if err := a.PushSubscribe("", sub); err == nil {
		t.Fatal("the desktop (no device) subscribed")
	}
	bad := sub
	bad.Endpoint = "http://push.example.net/a"
	if err := a.PushSubscribe(dev.ID, bad); err == nil {
		t.Fatal("a non-https endpoint was accepted")
	}
	if err := a.PushSubscribe(dev.ID, sub); err != nil {
		t.Fatal(err)
	}
	// Re-subscribing the same endpoint is an upsert, not a second row.
	if err := a.PushSubscribe(dev.ID, sub); err != nil || pushCount(t, a, dev.ID) != 1 {
		t.Fatalf("upsert: err %v, rows %d", err, pushCount(t, a, dev.ID))
	}
	devs, _ := a.RemoteDevices()
	for _, d := range devs {
		if d.Push != (d.ID == dev.ID) {
			t.Fatalf("device %s push = %v", d.Name, d.Push)
		}
	}

	// Another device cannot drop it.
	_ = a.PushUnsubscribe(other.ID, sub.Endpoint)
	if pushCount(t, a, dev.ID) != 1 {
		t.Fatal("another device unsubscribed this one")
	}
	if err := a.PushUnsubscribe(dev.ID, sub.Endpoint); err != nil || pushCount(t, a, dev.ID) != 0 {
		t.Fatalf("unsubscribe: err %v, rows %d", err, pushCount(t, a, dev.ID))
	}

	_ = a.PushSubscribe(dev.ID, sub)
	if err := a.RevokeRemoteDevice(dev.ID); err != nil {
		t.Fatal(err)
	}
	if pushCount(t, a, dev.ID) != 0 {
		t.Fatal("revoke left the device's push subscription behind")
	}
}

func TestPushGoneSubscriptionIsDeleted(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("Authorization") == "" {
			t.Errorf("missing push headers: %v", r.Header)
		}
		if r.URL.Path == "/gone" {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	a := newTestApp(t)
	a.push = newWebPusher(filepath.Join(t.TempDir(), "vapid.pem"))
	a.push.client = srv.Client()
	_, dev, _ := a.pairDevice("phone", "phone")
	_ = a.PushSubscribe(dev.ID, testSubscription(t, srv.URL+"/gone"))
	_ = a.PushSubscribe(dev.ID, testSubscription(t, srv.URL+"/live"))

	a.pushToAll([]byte(`{"title":"t"}`), "chat-1")
	if hits.Load() != 2 {
		t.Fatalf("push service saw %d requests, want 2", hits.Load())
	}
	var left string
	if err := a.db.QueryRow(`SELECT endpoint FROM push_subscriptions`).Scan(&left); err != nil || left != srv.URL+"/live" {
		t.Fatalf("after a 410 the table holds %q (err %v), want only /live", left, err)
	}
}
