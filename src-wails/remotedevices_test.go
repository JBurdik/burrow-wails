package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPairedDeviceIsFoundByItsToken(t *testing.T) {
	a := newTestApp(t)
	tok, dev, err := a.pairDevice("Jirka's phone", "phone")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := a.deviceForToken(tok)
	if !ok {
		t.Fatal("a freshly paired device did not authenticate")
	}
	if got.ID != dev.ID || got.Name != "Jirka's phone" || got.Kind != "phone" {
		t.Fatalf("wrong device: %+v", got)
	}
}

func TestUnknownTokenIsNotADevice(t *testing.T) {
	a := newTestApp(t)
	if _, _, err := a.pairDevice("phone", "phone"); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.deviceForToken("deadbeef"); ok {
		t.Fatal("an unknown token authenticated")
	}
	if _, ok := a.deviceForToken(""); ok {
		t.Fatal("an empty token authenticated")
	}
}

func TestRevokedDeviceNoLongerAuthenticates(t *testing.T) {
	// Revocation is the only lever the user has after a phone is lost, so
	// this is the test that matters most in this file.
	a := newTestApp(t)
	tok, dev, err := a.pairDevice("lost phone", "phone")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RevokeRemoteDevice(dev.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.deviceForToken(tok); ok {
		t.Fatal("a revoked device still authenticates")
	}
}

func TestRevokingAnUnknownDeviceIsAnError(t *testing.T) {
	// Settings needs to be able to say "that is already gone" rather than
	// reporting success for a row it never touched.
	a := newTestApp(t)
	if err := a.RevokeRemoteDevice("nope"); err == nil {
		t.Fatal("revoking a device that does not exist reported success")
	}
}

func TestTwoDevicesGetDifferentTokens(t *testing.T) {
	a := newTestApp(t)
	t1, d1, err := a.pairDevice("phone", "phone")
	if err != nil {
		t.Fatal(err)
	}
	t2, d2, err := a.pairDevice("tablet", "tablet")
	if err != nil {
		t.Fatal(err)
	}
	if t1 == t2 || d1.ID == d2.ID {
		t.Fatal("two devices shared a token or an id")
	}
	// And each token identifies its OWN device — the whole reason for
	// per-device tokens instead of one shared http.token.
	got1, _ := a.deviceForToken(t1)
	got2, _ := a.deviceForToken(t2)
	if got1.ID != d1.ID || got2.ID != d2.ID {
		t.Fatalf("tokens cross-identified: %s/%s", got1.ID, got2.ID)
	}
}

func TestRevokingOneDeviceLeavesTheOtherPaired(t *testing.T) {
	a := newTestApp(t)
	t1, d1, _ := a.pairDevice("phone", "phone")
	t2, _, _ := a.pairDevice("tablet", "tablet")
	if err := a.RevokeRemoteDevice(d1.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.deviceForToken(t1); ok {
		t.Fatal("the revoked device survived")
	}
	if _, ok := a.deviceForToken(t2); !ok {
		t.Fatal("revoking one device took the other with it")
	}
}

func TestDeviceListNeverCarriesATokenOrItsHash(t *testing.T) {
	// Settings renders this list; a token in it is a token in a screenshot.
	// Asserted on the marshalled JSON rather than the struct, because a
	// field added later without a `json:"-"` tag is exactly the mistake.
	a := newTestApp(t)
	tok, _, err := a.pairDevice("phone", "phone")
	if err != nil {
		t.Fatal(err)
	}
	list, err := a.RemoteDevices()
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), tok) {
		t.Fatalf("the device list carries the token: %s", blob)
	}
	if strings.Contains(string(blob), hashDeviceToken(tok)) {
		t.Fatalf("the device list carries the token hash: %s", blob)
	}
}

func TestDeviceListIsEmptyNotNullWithNoDevices(t *testing.T) {
	// "Nothing paired" and "the list failed to load" must not look the same
	// in Settings, and null renders as neither.
	a := newTestApp(t)
	list, err := a.RemoteDevices()
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(list)
	if string(blob) != "[]" {
		t.Fatalf("want [], got %s", blob)
	}
}

func TestTokenIsNotStoredInPlaintext(t *testing.T) {
	a := newTestApp(t)
	tok, _, err := a.pairDevice("phone", "phone")
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := a.db.QueryRow(`SELECT token_hash FROM remote_devices`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == tok {
		t.Fatal("the token is stored verbatim")
	}
	if stored != hashDeviceToken(tok) {
		t.Fatalf("stored value is neither the token nor its hash: %q", stored)
	}
}

func TestPairedDeviceGetsNeitherAccessWriteNorUiAck(t *testing.T) {
	// access:write is the pairing bootstrap — a device must not be able to
	// pair further devices. ui:ack is the desktop UI's identity claim, and
	// handing it out would let a client forge a reply to a control verb it
	// never performed (see remoteapi.go).
	a := newTestApp(t)
	_, dev, err := a.pairDevice("phone", "phone")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range dev.Scopes {
		if s == scopeAccessWrite || s == scopeUIAck {
			t.Fatalf("a paired device was granted %s", s)
		}
	}
	if len(dev.Scopes) == 0 {
		t.Fatal("a paired device got no scopes at all")
	}
}
