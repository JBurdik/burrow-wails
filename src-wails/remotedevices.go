package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Paired devices. One row per device the user let in, with its own token, so
// "revoke" means something narrower than "rotate the one shared secret and
// re-pair everything" — which is what the old http.token forced.
//
// The token is 32 random bytes, hex; the DB holds only its SHA-256. That is
// not a defence against someone already reading this disk (control.token sits
// next to it in plaintext), it is a defence against a usable token leaving in
// a DB backup, a sync folder or an error dump. It costs four lines.
//
// scopes live per row rather than as a constant in code, so narrowing what one
// device may do later is an UPDATE, not a migration.

// pairedDeviceScopes is what a device gets at pairing today.
//
// Deliberately NOT here: access:write, which is the pairing bootstrap (a
// device must not be able to pair further devices), and ui:ack, which is the
// desktop UI's identity claim (see remoteapi.go). A paired device can spawn a
// shell and reach both of those locally anyway — that is true, it is written
// out in remoteapi.go's note, and it is a reason not to CLAIM containment
// rather than a reason to hand out the names.
var pairedDeviceScopes = []remoteScope{scopeOrchRead, scopeOrchOperate, scopeTerminal}

// RemoteDevice is what Settings renders. It has no field for the token or its
// hash, so RemoteDevices() cannot return one by accident.
type RemoteDevice struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Kind     string        `json:"kind"`
	Scopes   []remoteScope `json:"scopes"`
	AddedAt  int64         `json:"added_at"`
	LastSeen int64         `json:"last_seen"`
}

func remoteDevicesSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS remote_devices (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			kind       TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			scopes     TEXT NOT NULL,
			added_at   INTEGER NOT NULL,
			last_seen  INTEGER NOT NULL
		)`,
	}
}

func hashDeviceToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func joinScopes(scopes []remoteScope) string {
	parts := make([]string, len(scopes))
	for i, s := range scopes {
		parts[i] = string(s)
	}
	return strings.Join(parts, ",")
}

func splitScopes(s string) []remoteScope {
	if s == "" {
		return nil
	}
	raw := strings.Split(s, ",")
	out := make([]remoteScope, 0, len(raw))
	for _, r := range raw {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, remoteScope(r))
		}
	}
	return out
}

// pairDevice records a new device and returns its token ONCE. There is no way
// to read it back afterwards, which is the point: a token the server can
// reproduce is a token the server can leak.
func (a *App) pairDevice(name, kind string) (string, RemoteDevice, error) {
	if a.db == nil {
		return "", RemoteDevice{}, fmt.Errorf("no database")
	}
	token := randomHex(32)
	if token == "" {
		return "", RemoteDevice{}, fmt.Errorf("could not generate a device token")
	}
	now := time.Now().UnixMilli()
	dev := RemoteDevice{
		ID:       randomHex(8),
		Name:     strings.TrimSpace(name),
		Kind:     strings.TrimSpace(kind),
		Scopes:   pairedDeviceScopes,
		AddedAt:  now,
		LastSeen: now,
	}
	if dev.ID == "" {
		return "", RemoteDevice{}, fmt.Errorf("could not generate a device id")
	}
	if dev.Name == "" {
		dev.Name = "Paired device"
	}
	if dev.Kind == "" {
		dev.Kind = "unknown"
	}
	_, err := a.db.Exec(
		`INSERT INTO remote_devices (id, name, kind, token_hash, scopes, added_at, last_seen)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		dev.ID, dev.Name, dev.Kind, hashDeviceToken(token), joinScopes(dev.Scopes), dev.AddedAt, dev.LastSeen,
	)
	if err != nil {
		return "", RemoteDevice{}, err
	}
	return token, dev, nil
}

// deviceForToken looks a device up by the hash of the presented token. A
// lookup by unique key, so there is no constant-time compare to make: the
// hash is what keeps the stored value from being a timing oracle, and an
// unknown token costs one index probe either way.
func (a *App) deviceForToken(token string) (RemoteDevice, bool) {
	if a.db == nil || token == "" {
		return RemoteDevice{}, false
	}
	var dev RemoteDevice
	var scopes string
	err := a.db.QueryRow(
		`SELECT id, name, kind, scopes, added_at, last_seen FROM remote_devices WHERE token_hash = ?`,
		hashDeviceToken(token),
	).Scan(&dev.ID, &dev.Name, &dev.Kind, &scopes, &dev.AddedAt, &dev.LastSeen)
	if err != nil {
		return RemoteDevice{}, false
	}
	dev.Scopes = splitScopes(scopes)
	return dev, true
}

// touchDevice records that a device was seen. Best effort: a failed write
// here must not cost the device its connection.
func (a *App) touchDevice(id string) {
	if a.db == nil {
		return
	}
	_, _ = a.db.Exec(`UPDATE remote_devices SET last_seen = ? WHERE id = ?`, time.Now().UnixMilli(), id)
}

// RemoteDevices lists the paired devices for Settings, newest first.
func (a *App) RemoteDevices() ([]RemoteDevice, error) {
	// Empty, not nil: Settings has to be able to tell "nothing paired" from
	// "the list failed to load", and a null renders as neither.
	out := []RemoteDevice{}
	if a.db == nil {
		return out, nil
	}
	rows, err := a.db.Query(
		`SELECT id, name, kind, scopes, added_at, last_seen FROM remote_devices ORDER BY added_at DESC`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var dev RemoteDevice
		var scopes string
		if err := rows.Scan(&dev.ID, &dev.Name, &dev.Kind, &scopes, &dev.AddedAt, &dev.LastSeen); err != nil {
			return out, err
		}
		dev.Scopes = splitScopes(scopes)
		out = append(out, dev)
	}
	return out, rows.Err()
}

// RevokeRemoteDevice removes a device and drops whatever it currently has
// open. A revoke that leaves yesterday's socket running is not a revoke — and
// that socket is the whole app.
func (a *App) RevokeRemoteDevice(id string) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	res, err := a.db.Exec(`DELETE FROM remote_devices WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return sql.ErrNoRows
	}
	// After the row is gone, so a connection racing this cannot re-authorize
	// itself against a row that still exists.
	if a.remoteWS != nil {
		a.remoteWS.dropDevice(id)
	}
	return nil
}
