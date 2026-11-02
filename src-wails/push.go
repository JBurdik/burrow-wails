package main

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Push subscriptions: one row per browser subscription, owned by the paired
// device that registered it. The device id is the CONNECTION's (remotews.go
// injects `_device`), never something the phone claims — so a phone can only
// add or drop its own, and the desktop (device "") has none.

func pushSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS push_subscriptions (
			endpoint   TEXT PRIMARY KEY,
			device_id  TEXT NOT NULL,
			p256dh     TEXT NOT NULL,
			auth       TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS push_subscriptions_device ON push_subscriptions(device_id)`,
	}
}

// webPusher holds the VAPID key and the per-key push state. nil-safe where
// the chat runtime calls it, so an App without one (tests) just never pushes.
type webPusher struct {
	keyPath string
	client  *http.Client

	keyMu sync.Mutex
	key   *ecdsa.PrivateKey

	mu      sync.Mutex
	last    map[string]time.Time // phase key -> last push sent
	watched map[string]time.Time // phase key -> last chat_mark_active
}

func newWebPusher(keyPath string) *webPusher {
	return &webPusher{
		keyPath: keyPath,
		client:  &http.Client{Timeout: 15 * time.Second},
		last:    map[string]time.Time{},
		watched: map[string]time.Time{},
	}
}

func (p *webPusher) vapidKey() (*ecdsa.PrivateKey, error) {
	p.keyMu.Lock()
	defer p.keyMu.Unlock()
	if p.key == nil {
		k, err := loadOrCreateVAPIDKey(p.keyPath)
		if err != nil {
			return nil, err
		}
		p.key = k
	}
	return p.key, nil
}

var errNoPush = errors.New("push notifications unavailable")

// PushVapidPublicKey is the applicationServerKey a phone subscribes with.
func (a *App) PushVapidPublicKey() (string, error) {
	if a.push == nil {
		return "", errNoPush
	}
	k, err := a.push.vapidKey()
	if err != nil {
		return "", err
	}
	return vapidPublicKey(k)
}

// PushSubscribe stores the calling device's subscription (upsert by endpoint:
// a browser re-subscribing hands back the same endpoint with fresh keys).
func (a *App) PushSubscribe(deviceID string, sub pushSubscription) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	if deviceID == "" {
		return fmt.Errorf("only a paired device can subscribe to push")
	}
	if err := validPushSubscription(sub); err != nil {
		return err
	}
	_, err := a.db.Exec(`INSERT INTO push_subscriptions (endpoint, device_id, p256dh, auth, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET device_id = excluded.device_id, p256dh = excluded.p256dh, auth = excluded.auth`,
		sub.Endpoint, deviceID, sub.Keys.P256dh, sub.Keys.Auth, time.Now().UnixMilli())
	return err
}

// PushUnsubscribe drops one of the calling device's subscriptions.
func (a *App) PushUnsubscribe(deviceID, endpoint string) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	_, err := a.db.Exec(`DELETE FROM push_subscriptions WHERE endpoint = ? AND device_id = ?`, endpoint, deviceID)
	return err
}

// pushToAll sends one message to every subscription; a 404/410 forgets it.
func (a *App) pushToAll(payload []byte, topic string) {
	if a.push == nil || a.db == nil {
		return
	}
	rows, err := a.db.Query(`SELECT endpoint, p256dh, auth FROM push_subscriptions`)
	if err != nil {
		return
	}
	var subs []pushSubscription
	for rows.Next() {
		var s pushSubscription
		if rows.Scan(&s.Endpoint, &s.Keys.P256dh, &s.Keys.Auth) == nil {
			subs = append(subs, s)
		}
	}
	rows.Close()
	if len(subs) == 0 {
		return
	}
	k, err := a.push.vapidKey()
	if err != nil {
		log.Printf("push: vapid key: %v", err)
		return
	}
	for _, s := range subs {
		err := sendWebPush(a.push.client, k, s, payload, topic)
		if errors.Is(err, errPushGone) {
			_, _ = a.db.Exec(`DELETE FROM push_subscriptions WHERE endpoint = ?`, s.Endpoint)
		} else if err != nil {
			log.Printf("push: %v", err)
		}
	}
}
