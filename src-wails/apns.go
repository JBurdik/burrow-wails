package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// APNs: the native iOS app's (ios/) second delivery kind next to Web Push,
// fed by the same decisions (pushFor). HTTP/2 + token auth: an ES256 JWT
// signed with the team's .p8 key. Stdlib only — net/http negotiates HTTP/2.
//
// Config: <app-data>/apns.p8 (0600) + <app-data>/apns.json {keyId, teamId,
// topic}; either missing = APNs silently off. Read once at startup. Neither
// the key nor a device token is ever logged (a url.Error would carry the
// token in its URL, so transport errors are unwrapped).

func apnsSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS apns_tokens (
			token      TEXT PRIMARY KEY,
			device_id  TEXT NOT NULL,
			sandbox    INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS apns_tokens_device ON apns_tokens(device_id)`,
	}
}

type apnsConfig struct {
	KeyID  string `json:"keyId"`
	TeamID string `json:"teamId"`
	Topic  string `json:"topic"` // the app's bundle id
}

type apnsSender struct {
	cfg    apnsConfig
	key    *ecdsa.PrivateKey
	client *http.Client
	// hosts: production, sandbox. Fields only so a test can point them at httptest.
	prodURL, sandboxURL string

	mu    sync.Mutex
	jwt   string
	jwtAt time.Time
}

// loadAPNs returns nil, nil when the files are absent (APNs off).
func loadAPNs(dataDir string, client *http.Client) (*apnsSender, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, "apns.p8"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	cj, err := os.ReadFile(filepath.Join(dataDir, "apns.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var cfg apnsConfig
	if err := json.Unmarshal(cj, &cfg); err != nil {
		return nil, fmt.Errorf("apns.json: %w", err)
	}
	if cfg.KeyID == "" || cfg.TeamID == "" || cfg.Topic == "" {
		return nil, fmt.Errorf("apns.json: keyId, teamId and topic are required")
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("apns.p8: not PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns.p8: %w", err)
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("apns.p8: not an EC key")
	}
	return &apnsSender{cfg: cfg, key: ec, client: client,
		prodURL: "https://api.push.apple.com", sandboxURL: "https://api.sandbox.push.apple.com"}, nil
}

// apnsJWT is the provider token: header {alg, kid}, claims {iss, iat}.
func apnsJWT(k *ecdsa.PrivateKey, keyID, teamID string, now time.Time) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": keyID})
	claims, _ := json.Marshal(map[string]any{"iss": teamID, "iat": now.Unix()})
	signing := b64.EncodeToString(header) + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, k, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64) // JWS ES256: r||s, not ASN.1
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + b64.EncodeToString(sig), nil
}

// token reuses one JWT for 50 min: Apple refuses one older than an hour and
// throttles (TooManyProviderTokenUpdates) a fresh one per request.
func (s *apnsSender) token(now time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jwt != "" && now.Sub(s.jwtAt) < 50*time.Minute {
		return s.jwt, nil
	}
	t, err := apnsJWT(s.key, s.cfg.KeyID, s.cfg.TeamID, now)
	if err != nil {
		return "", err
	}
	s.jwt, s.jwtAt = t, now
	return t, nil
}

func (s *apnsSender) resetToken() {
	s.mu.Lock()
	s.jwt = ""
	s.mu.Unlock()
}

// pushMsg is what pushFor decided to say; each delivery kind shapes it.
type pushMsg struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"` // hash route, "#/ws/3?chat=7"
	Tag   string `json:"tag"` // phase key, "chat:7"
}

// apnsPayload: alert + sound, thread-id per chat/tab (Notification Center
// groups by it), and the route the app opens on a tap.
func apnsPayload(m pushMsg) []byte {
	b, _ := json.Marshal(map[string]any{
		"aps": map[string]any{
			"alert":     map[string]string{"title": m.Title, "body": m.Body},
			"sound":     "default",
			"thread-id": m.Tag,
		},
		"url": m.URL,
	})
	return b
}

var errAPNsGone = errors.New("apns token gone")

// apnsGoneReasons: the token will never work again — forget the row.
// (DeviceTokenNotForTopic / a sandbox-production mismatch also answer
// BadDeviceToken; the app re-registers on its next connect.)
var apnsGoneReasons = map[string]bool{"BadDeviceToken": true, "Unregistered": true, "DeviceTokenNotForTopic": true}

func (s *apnsSender) send(token string, sandbox bool, payload []byte, collapseID string) error {
	jwt, err := s.token(time.Now())
	if err != nil {
		return err
	}
	host := s.prodURL
	if sandbox {
		host = s.sandboxURL
	}
	req, err := http.NewRequest(http.MethodPost, host+"/3/device/"+token, bytes.NewReader(payload))
	if err != nil {
		return errors.New("apns: bad request")
	}
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", s.cfg.Topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("apns-expiration", fmt.Sprint(time.Now().Add(24*time.Hour).Unix()))
	if collapseID != "" {
		req.Header.Set("apns-collapse-id", collapseID)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // drop the URL: it holds the device token
		}
		return fmt.Errorf("apns: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode == http.StatusGone || apnsGoneReasons[body.Reason] {
		return errAPNsGone
	}
	if body.Reason == "ExpiredProviderToken" || body.Reason == "InvalidProviderToken" {
		s.resetToken()
	}
	return fmt.Errorf("apns: %s %s", resp.Status, body.Reason)
}

var apnsTokenRe = regexp.MustCompile(`^[0-9a-f]{64,200}$`)

// PushSubscribeAPNs stores the calling device's APNs token (hex). sandbox =
// the build's aps-environment is development.
func (a *App) PushSubscribeAPNs(deviceID, token string, sandbox bool) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	if deviceID == "" {
		return fmt.Errorf("only a paired device can subscribe to push")
	}
	token = strings.ToLower(token)
	if !apnsTokenRe.MatchString(token) {
		return fmt.Errorf("invalid APNs device token")
	}
	_, err := a.db.Exec(`INSERT INTO apns_tokens (token, device_id, sandbox, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(token) DO UPDATE SET device_id = excluded.device_id, sandbox = excluded.sandbox`,
		token, deviceID, sandbox, time.Now().UnixMilli())
	return err
}

// PushUnsubscribeAPNs drops one of the calling device's tokens.
func (a *App) PushUnsubscribeAPNs(deviceID, token string) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	_, err := a.db.Exec(`DELETE FROM apns_tokens WHERE token = ? AND device_id = ?`, strings.ToLower(token), deviceID)
	return err
}
