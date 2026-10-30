package main

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatalf("%q: %v", s, err)
	}
	return b
}

// RFC 8291 §5 / Appendix A — the one test that says the encryption is right.
func TestEncryptPushPayloadRFC8291Vector(t *testing.T) {
	asPriv, err := ecdh.P256().NewPrivateKey(mustB64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := encryptPushPayload(
		[]byte("When I grow up, I want to be a watermelon"),
		mustB64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcx aOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"),
		mustB64(t, "BTBZMqHH6r4Tts7J_aSIgg"),
		asPriv,
		mustB64(t, "DGv6ra1nlYgDCS1FRnbzlw"),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := `DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml
		mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT
		pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN`
	if b64.EncodeToString(got) != strings.Join(strings.Fields(want), "") {
		t.Fatalf("ciphertext mismatch:\n got %s", b64.EncodeToString(got))
	}
}

func TestVAPIDKeyPersistsAndSignsVerifiably(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vapid.pem")
	k1, err := loadOrCreateVAPIDKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v, want 0600", fi.Mode().Perm())
	}
	k2, err := loadOrCreateVAPIDKey(path)
	if err != nil || !k1.Equal(k2) {
		t.Fatalf("second load returned a different key (err %v)", err)
	}

	h, err := vapidAuthorization(k1, "https://push.example.net/abc?x=1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tok, pub, ok := strings.Cut(strings.TrimPrefix(h, "vapid t="), ", k=")
	if !ok {
		t.Fatalf("bad header %q", h)
	}
	if want, _ := vapidPublicKey(k1); pub != want {
		t.Fatal("k= is not the VAPID public key")
	}
	parts := strings.Split(tok, ".")
	var claims struct {
		Aud string `json:"aud"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(mustB64(t, parts[1]), &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Aud != "https://push.example.net" || claims.Sub == "" || claims.Exp <= time.Now().Unix() {
		t.Fatalf("claims %+v", claims)
	}
	sig := mustB64(t, parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(&k1.PublicKey, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("JWT signature does not verify")
	}
}
