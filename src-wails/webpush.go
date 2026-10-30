package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Web Push (RFC 8030) to the paired phone PWA, straight from this process to
// the browser vendor's push service — no relay of ours in between. Payloads
// are encrypted per RFC 8291 (aes128gcm) and the request is signed with a
// VAPID JWT (RFC 8292, ES256).
//
// ponytail: stdlib only (crypto/ecdh + crypto/hkdf + crypto/ecdsa) rather than
// SherClockHolmes/webpush-go — the whole protocol is ~150 lines and the
// library would bring golang-jwt along for one signature. The RFC 8291
// Appendix A vector in webpush_test.go is what keeps it honest.

var b64 = base64.RawURLEncoding

// vapidSubject is the contact the push service may use. Apple rejects a JWT
// without a valid mailto:/https: sub.
const vapidSubject = "https://github.com/JBurdik/burrow-wails"

// loadOrCreateVAPIDKey reads the P-256 key at path, generating it once (0600).
// The key is the identity every subscription is bound to: regenerating it
// silently orphans every phone's subscription, so it is never rotated here.
func loadOrCreateVAPIDKey(path string) (*ecdsa.PrivateKey, error) {
	if raw, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil, fmt.Errorf("%s: not PEM", path)
		}
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		ec, ok := k.(*ecdsa.PrivateKey)
		if !ok || ec.Curve != elliptic.P256() {
			return nil, fmt.Errorf("%s: not a P-256 key", path)
		}
		return ec, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// O_EXCL: two first calls racing must not each write a different key.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return loadOrCreateVAPIDKey(path)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		return nil, err
	}
	return k, nil
}

// vapidPublicKey is the uncompressed point, base64url — the
// applicationServerKey the browser's pushManager.subscribe wants.
func vapidPublicKey(k *ecdsa.PrivateKey) (string, error) {
	pub, err := k.PublicKey.ECDH()
	if err != nil {
		return "", err
	}
	return b64.EncodeToString(pub.Bytes()), nil
}

// vapidAuthorization builds the `Authorization: vapid t=…, k=…` header for an
// endpoint (aud = its origin).
func vapidAuthorization(k *ecdsa.PrivateKey, endpoint string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		"exp": now.Add(12 * time.Hour).Unix(),
		"sub": vapidSubject,
	})
	signing := header + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, k, digest[:])
	if err != nil {
		return "", err
	}
	// JWS ES256 is r||s, each left-padded to 32 bytes — not ASN.1.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	pub, err := vapidPublicKey(k)
	if err != nil {
		return "", err
	}
	return "vapid t=" + signing + "." + b64.EncodeToString(sig) + ", k=" + pub, nil
}

// encryptPushPayload is RFC 8291 §3.4 with a single aes128gcm record
// (rs 4096, no padding). asPriv and salt are fresh per message; they are
// parameters only so the RFC's own vector can be replayed.
func encryptPushPayload(plaintext, uaPublic, authSecret []byte, asPriv *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(plaintext) > 4096-16-1 {
		return nil, fmt.Errorf("push payload too large (%d bytes)", len(plaintext))
	}
	uaPub, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("p256dh: %w", err)
	}
	ecdhSecret, err := asPriv.ECDH(uaPub)
	if err != nil {
		return nil, err
	}
	asPublic := asPriv.PublicKey().Bytes()
	keyInfo := "WebPush: info\x00" + string(uaPublic) + string(asPublic)
	ikm, err := hkdf.Key(sha256.New, ecdhSecret, authSecret, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.Write(salt)
	_ = binary.Write(&out, binary.BigEndian, uint32(4096))
	out.WriteByte(byte(len(asPublic)))
	out.Write(asPublic)
	// 0x02 = last-record padding delimiter.
	return gcm.Seal(out.Bytes(), nonce, append(append([]byte{}, plaintext...), 0x02), nil), nil
}

// pushSubscription is the browser's PushSubscription.toJSON() shape.
type pushSubscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// errPushGone is a 404/410 from the push service: the subscription is dead
// and must be forgotten.
var errPushGone = errors.New("push subscription gone")

// sendWebPush encrypts and POSTs one message. topic collapses still-queued
// messages at the push service (url-safe, ≤ 32 chars).
func sendWebPush(client *http.Client, k *ecdsa.PrivateKey, sub pushSubscription, payload []byte, topic string) error {
	uaPublic, err := b64.DecodeString(sub.Keys.P256dh)
	if err != nil {
		return fmt.Errorf("p256dh: %w", err)
	}
	authSecret, err := b64.DecodeString(sub.Keys.Auth)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	asPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	body, err := encryptPushPayload(payload, uaPublic, authSecret, asPriv, salt)
	if err != nil {
		return err
	}
	authz, err := vapidAuthorization(k, sub.Endpoint, time.Now())
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authz)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", "86400")
	req.Header.Set("Urgency", "high")
	if topic != "" {
		req.Header.Set("Topic", topic)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return errPushGone
	case resp.StatusCode >= 300:
		return fmt.Errorf("push service: %s", resp.Status)
	}
	return nil
}

// validPushSubscription is the trust-boundary check on what a phone hands us:
// an https endpoint (we POST to it from the desktop) and keys of the right size.
func validPushSubscription(sub pushSubscription) error {
	u, err := url.Parse(sub.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("push endpoint must be an https URL")
	}
	if p, err := b64.DecodeString(sub.Keys.P256dh); err != nil || len(p) != 65 {
		return fmt.Errorf("invalid p256dh key")
	}
	if a, err := b64.DecodeString(sub.Keys.Auth); err != nil || len(a) != 16 {
		return fmt.Errorf("invalid auth secret")
	}
	return nil
}

