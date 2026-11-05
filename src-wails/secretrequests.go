package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"burrow/internal/agentphase"
)

// Secret requests: an agent asks the user for a secret (`burrow
// secret-request`, the secret_request verb) and gets back ONLY a file path.
//
// The value travels exactly one way: client → answer_secret_request → a 0600
// file under <app-data>/secrets/turn. It is never logged, never emitted on the
// bus, never written to chat_stream / chat_messages / SQLite / config.json and
// never part of a verb result. The pending list published to clients
// (`secret-requests`, list_secret_requests) carries name + reason + hasSaved,
// nothing else.
//
// A delivered file lives until the owner's turn ends (PhaseStore.onChange →
// turnEnded), at most secretFileTTL (sweep), and never past an app restart
// (startup wipes the turn dir).
//
// "Remember for this project" additionally stores NAME=value in
// <app-data>/secrets/project-<repoId>.env (repo = the root workspace). A saved
// value is likewise never sent to a client: "Use saved" is a flag on the
// answer and Go reads the file itself. Nothing is ever fulfilled without a
// click.

const (
	secretRequestTimeout = 15 * time.Minute
	secretFileTTL        = 30 * time.Minute
)

var secretNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func validSecretName(name string) error {
	if !secretNameRe.MatchString(name) {
		return fmt.Errorf("secret name must match [A-Za-z0-9_.-]{1,64}")
	}
	return nil
}

var errSecretDeclined = errors.New("the user declined to provide the secret")

type secretRequest struct {
	ID       string `json:"id"`
	Owner    string `json:"owner"` // chat:<id> | pty:<id>
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	At       int64  `json:"at"`
	RepoID   int64  `json:"repo_id"` // 0 = no project, nothing can be remembered
	HasSaved bool   `json:"hasSaved"`

	done   chan secretResult // buffered 1
	marked bool              // we put the owner's phase into waiting_input
}

type secretResult struct {
	path string
	err  error
}

type deliveredSecret struct {
	path string
	at   time.Time
}

type secretRegistry struct {
	dir    string // <app-data>/secrets
	phases *PhaseStore
	emit   func(list []secretRequest)
	// note records a name-only transcript row for a chat owner (nil in tests).
	note func(owner, text string)

	mu    sync.Mutex
	reqs  []*secretRequest
	files map[string][]deliveredSecret // owner → delivered files

	savedMu sync.Mutex // project-*.env read-modify-write
}

func newSecretRegistry(dir string, phases *PhaseStore) *secretRegistry {
	r := &secretRegistry{dir: dir, phases: phases, files: map[string][]deliveredSecret{}}
	r.emit = func(list []secretRequest) { busEmit("secret-requests", map[string]any{"requests": list}) }
	// Per-turn delivery files never outlive the process; saved project-*.env
	// files sit beside the turn dir and are left alone.
	_ = os.RemoveAll(r.turnDir())
	return r
}

func (r *secretRegistry) turnDir() string { return filepath.Join(r.dir, "turn") }

// listLocked is the pending list as published: values never exist here.
func (r *secretRegistry) listLocked() []secretRequest {
	out := make([]secretRequest, 0, len(r.reqs))
	for _, q := range r.reqs {
		c := *q
		c.done = nil
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

func (r *secretRegistry) List() []secretRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listLocked()
}

func (r *secretRegistry) publish() {
	r.mu.Lock()
	list := r.listLocked()
	r.mu.Unlock()
	r.emit(list)
}

// Request parks until the user answers, declines, the owner's turn ends, the
// caller goes away or secretRequestTimeout passes. It returns the file path.
func (r *secretRegistry) Request(ctx context.Context, owner, name, reason string, repoID int64) (string, error) {
	if err := validSecretName(name); err != nil {
		return "", err
	}
	if !strings.HasPrefix(owner, "chat:") && !strings.HasPrefix(owner, "pty:") {
		return "", fmt.Errorf("secret_request: unknown caller %q", owner)
	}
	req := &secretRequest{
		ID: randHex(8), Owner: owner, Name: name, Reason: truncateRunes(strings.TrimSpace(reason), 500, "…"),
		At: time.Now().UnixMilli(), RepoID: repoID, done: make(chan secretResult, 1),
	}
	if repoID > 0 {
		req.HasSaved = r.hasSaved(repoID, name)
	}
	// Only a turn in flight is marked waiting: a plain shell calling the CLI
	// by hand has no turn to block, and resuming it later would invent one.
	if r.phases != nil && r.phases.Get(owner).InFlight() {
		req.marked = true
	}
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	r.publish()
	if req.marked {
		r.phases.Apply(owner, agentphase.Event{Kind: agentphase.ProgramStatus, State: agentphase.WaitingInput, Detail: "Secret requested: " + name})
	}

	timer := time.NewTimer(secretRequestTimeout)
	defer timer.Stop()
	var res secretResult
	select {
	case res = <-req.done:
	case <-ctx.Done():
		res = r.finish(req.ID, secretResult{err: fmt.Errorf("secret request cancelled")})
	case <-timer.C:
		res = r.finish(req.ID, secretResult{err: fmt.Errorf("secret request timed out after %s with no answer", secretRequestTimeout)})
	}
	return res.path, res.err
}

// finish closes a pending request with res (first finisher wins: a request
// already closed returns the result it got) and puts the owner's phase back.
func (r *secretRegistry) finish(id string, res secretResult) secretResult {
	r.mu.Lock()
	var req *secretRequest
	for i, q := range r.reqs {
		if q.ID == id {
			req = q
			r.reqs = append(r.reqs[:i], r.reqs[i+1:]...)
			break
		}
	}
	r.mu.Unlock()
	if req == nil {
		return res
	}
	req.done <- res
	r.publish()
	r.unmark(req)
	if r.note != nil && strings.HasPrefix(req.Owner, "chat:") {
		switch {
		case res.path != "":
			r.note(req.Owner, "Secret "+req.Name+" provided")
		case errors.Is(res.err, errSecretDeclined):
			r.note(req.Owner, "Secret "+req.Name+" declined")
		}
	}
	return res
}

func (r *secretRegistry) unmark(req *secretRequest) {
	if req.marked && r.phases != nil {
		// Resume: waiting_input → running; a no-op if the turn moved on.
		r.phases.Apply(req.Owner, agentphase.Event{Kind: agentphase.Resume})
	}
}

func (r *secretRegistry) pending(id string) (secretRequest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, q := range r.reqs {
		if q.ID == id {
			return *q, true
		}
	}
	return secretRequest{}, false
}

// Answer delivers a value: typed (value), or the project's saved one
// (useSaved). remember stores a typed value for the project.
func (r *secretRegistry) Answer(id, value string, useSaved, remember bool) error {
	req, ok := r.pending(id)
	if !ok {
		return fmt.Errorf("secret request %s is no longer pending", id)
	}
	if useSaved {
		if req.RepoID <= 0 {
			return fmt.Errorf("no project to read a saved secret from")
		}
		saved, ok, err := r.savedValue(req.RepoID, req.Name)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("no saved value for %s", req.Name)
		}
		value = saved
	} else {
		if value == "" {
			return fmt.Errorf("empty secret")
		}
		if remember {
			if req.RepoID <= 0 {
				return fmt.Errorf("this request has no project to remember the secret for")
			}
			if err := r.saveSecret(req.RepoID, req.Name, value); err != nil {
				return err
			}
		}
	}
	path, err := r.writeDelivery(req.Name, value)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.files[req.Owner] = append(r.files[req.Owner], deliveredSecret{path: path, at: time.Now()})
	r.mu.Unlock()
	if got := r.finish(id, secretResult{path: path}); got.path != path {
		// Lost a race with another answer / the timeout: drop our copy.
		r.removeFile(req.Owner, path)
		return fmt.Errorf("secret request %s is no longer pending", id)
	}
	return nil
}

func (r *secretRegistry) Decline(id string) error {
	if _, ok := r.pending(id); !ok {
		return fmt.Errorf("secret request %s is no longer pending", id)
	}
	r.finish(id, secretResult{err: errSecretDeclined})
	return nil
}

// writeDelivery creates <turn>/<random>-<name>, 0600, in a 0700 dir, never
// over an existing file.
func (r *secretRegistry) writeDelivery(name, value string) (string, error) {
	dir := r.turnDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("secret dir: %w", err)
	}
	_ = os.Chmod(r.dir, 0o700)
	_ = os.Chmod(dir, 0o700)
	path := filepath.Join(dir, randHex(12)+"-"+name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("secret file: %w", err)
	}
	_, werr := f.WriteString(value)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("secret file: write failed")
	}
	return path, nil
}

func (r *secretRegistry) removeFile(owner, path string) {
	_ = os.Remove(path)
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.files[owner][:0]
	for _, f := range r.files[owner] {
		if f.path != path {
			list = append(list, f)
		}
	}
	r.files[owner] = list
}

// phaseChanged runs from PhaseStore.onChange (under emitMu — must not Apply):
// a turn leaving flight ends every pending request of that owner and deletes
// what was delivered to it.
func (r *secretRegistry) phaseChanged(key string, prev, next agentphase.Phase) {
	if !prev.InFlight() || next.InFlight() {
		return
	}
	r.mu.Lock()
	var ended []*secretRequest
	keep := r.reqs[:0]
	for _, q := range r.reqs {
		if q.Owner == key {
			ended = append(ended, q)
		} else {
			keep = append(keep, q)
		}
	}
	r.reqs = keep
	files := r.files[key]
	delete(r.files, key)
	r.mu.Unlock()
	for _, f := range files {
		_ = os.Remove(f.path)
	}
	for _, q := range ended {
		q.done <- secretResult{err: fmt.Errorf("the turn ended before the secret was provided")}
	}
	if len(ended) > 0 {
		go r.publish() // emit takes the bus; keep it off emitMu's critical path
	}
}

// sweep deletes delivered files older than ttl.
func (r *secretRegistry) sweep(now time.Time, ttl time.Duration) {
	r.mu.Lock()
	var old []string
	for owner, list := range r.files {
		keep := list[:0]
		for _, f := range list {
			if now.Sub(f.at) >= ttl {
				old = append(old, f.path)
			} else {
				keep = append(keep, f)
			}
		}
		if len(keep) == 0 {
			delete(r.files, owner)
		} else {
			r.files[owner] = keep
		}
	}
	r.mu.Unlock()
	for _, p := range old {
		_ = os.Remove(p)
	}
}

func (r *secretRegistry) sweepLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for now := range t.C {
		r.sweep(now, secretFileTTL)
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- saved project secrets (dotenv) -------------------------------------------

type savedSecret struct {
	Name    string `json:"name"`
	Created int64  `json:"created"` // unix ms
	Updated int64  `json:"updated"`

	value string // never serialized: unexported
}

func (r *secretRegistry) projectFile(repoID int64) string {
	return filepath.Join(r.dir, "project-"+strconv.FormatInt(repoID, 10)+".env")
}

// dotenvQuote writes a double-quoted dotenv value: \\ \" \n \r \t \$ escaped,
// so a multi-line or quote-bearing secret round-trips and no loader expands
// `$VAR` inside it.
func dotenvQuote(v string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range v {
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '$':
			b.WriteString(`\$`)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func dotenvUnquote(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return strings.TrimSpace(s)
	}
	s = s[1 : len(s)-1]
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

const savedMetaPrefix = "# burrow:"

func parseSavedSecrets(data string) []savedSecret {
	var out []savedSecret
	var created, updated int64
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if meta, ok := strings.CutPrefix(line, savedMetaPrefix); ok {
			created, updated = 0, 0
			for _, f := range strings.Fields(meta) {
				k, v, _ := strings.Cut(f, "=")
				n, _ := strconv.ParseInt(v, 10, 64)
				switch k {
				case "created":
					created = n
				case "updated":
					updated = n
				}
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || validSecretName(k) != nil {
			continue
		}
		out = append(out, savedSecret{Name: k, Created: created, Updated: updated, value: dotenvUnquote(v)})
		created, updated = 0, 0
	}
	return out
}

func formatSavedSecrets(list []savedSecret) string {
	var b strings.Builder
	b.WriteString("# Saved by Burrow (Remember for this project). Values are secrets.\n")
	for _, s := range list {
		fmt.Fprintf(&b, "%s created=%d updated=%d\n%s=%s\n", savedMetaPrefix, s.Created, s.Updated, s.Name, dotenvQuote(s.value))
	}
	return b.String()
}

func (r *secretRegistry) readSavedLocked(repoID int64) ([]savedSecret, error) {
	data, err := os.ReadFile(r.projectFile(repoID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read saved secrets: %w", err)
	}
	return parseSavedSecrets(string(data)), nil
}

// writeSavedLocked replaces the project file atomically (temp + rename, 0600
// in a 0700 dir); an empty list removes it.
func (r *secretRegistry) writeSavedLocked(repoID int64, list []savedSecret) error {
	path := r.projectFile(repoID)
	if len(list) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(r.dir, 0o700)
	f, err := os.CreateTemp(r.dir, ".project-*.tmp") // 0600
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.WriteString(formatSavedSecrets(list))
	serr := f.Sync()
	cerr := f.Close()
	if werr != nil || serr != nil || cerr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write saved secrets failed")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (r *secretRegistry) saveSecret(repoID int64, name, value string) error {
	if err := validSecretName(name); err != nil {
		return err
	}
	r.savedMu.Lock()
	defer r.savedMu.Unlock()
	list, err := r.readSavedLocked(repoID)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	found := false
	for i := range list {
		if list[i].Name == name {
			list[i].value, list[i].Updated = value, now
			found = true
		}
	}
	if !found {
		list = append(list, savedSecret{Name: name, Created: now, Updated: now, value: value})
	}
	return r.writeSavedLocked(repoID, list)
}

func (r *secretRegistry) savedValue(repoID int64, name string) (string, bool, error) {
	r.savedMu.Lock()
	defer r.savedMu.Unlock()
	list, err := r.readSavedLocked(repoID)
	if err != nil {
		return "", false, err
	}
	for _, s := range list {
		if s.Name == name {
			return s.value, true, nil
		}
	}
	return "", false, nil
}

func (r *secretRegistry) hasSaved(repoID int64, name string) bool {
	_, ok, _ := r.savedValue(repoID, name)
	return ok
}

// ListSaved returns names and dates only (value is unexported, never marshalled).
func (r *secretRegistry) ListSaved(repoID int64) ([]savedSecret, error) {
	r.savedMu.Lock()
	defer r.savedMu.Unlock()
	list, err := r.readSavedLocked(repoID)
	out := make([]savedSecret, 0, len(list))
	for _, s := range list {
		out = append(out, savedSecret{Name: s.Name, Created: s.Created, Updated: s.Updated})
	}
	return out, err
}

func (r *secretRegistry) DeleteSaved(repoID int64, name string) error {
	r.savedMu.Lock()
	defer r.savedMu.Unlock()
	list, err := r.readSavedLocked(repoID)
	if err != nil {
		return err
	}
	keep := list[:0]
	for _, s := range list {
		if s.Name != name {
			keep = append(keep, s)
		}
	}
	if err := r.writeSavedLocked(repoID, keep); err != nil {
		return err
	}
	// A pending card's "Saved value available" just went stale.
	r.mu.Lock()
	changed := false
	for _, q := range r.reqs {
		if q.RepoID == repoID && q.Name == name && q.HasSaved {
			q.HasSaved = false
			changed = true
		}
	}
	r.mu.Unlock()
	if changed {
		r.publish()
	}
	return nil
}

// --- App surface ---------------------------------------------------------------

// secretOwnerRepo resolves the owner's project: the root workspace
// (parent_id ?? id) of the chat's / tab's workspace. 0 = unknown.
func (a *App) secretOwnerRepo(owner string) int64 {
	if a.db == nil {
		return 0
	}
	kind, id, _ := strings.Cut(owner, ":")
	var ws int64
	switch kind {
	case "chat":
		_ = a.db.QueryRow(`SELECT workspace_id FROM chats WHERE id = ?`, id).Scan(&ws)
	case "pty":
		_ = a.db.QueryRow(`SELECT workspace_id FROM terminal_tabs WHERE pty_id = ? ORDER BY id DESC LIMIT 1`, id).Scan(&ws)
	}
	if ws <= 0 {
		return 0
	}
	var repo int64
	if err := a.db.QueryRow(`SELECT COALESCE(parent_id, id) FROM workspaces WHERE id = ?`, ws).Scan(&repo); err != nil {
		return 0
	}
	return repo
}

// RequestSecret is the secret_request verb's capability (control.Secrets).
func (a *App) RequestSecret(ctx context.Context, owner, name, reason string) (string, error) {
	if a.secrets == nil {
		return "", fmt.Errorf("secret requests not ready")
	}
	return a.secrets.Request(ctx, owner, name, reason, a.secretOwnerRepo(owner))
}

// ListSecretRequests is the first paint of pending secret requests (no values);
// later changes arrive as `secret-requests` events.
func (a *App) ListSecretRequests() []secretRequest {
	if a.secrets == nil {
		return []secretRequest{}
	}
	return a.secrets.List()
}

// AnswerSecretRequest delivers a typed value (remember = also save it for the
// project) or, with useSaved, the project's saved value read by Go itself.
func (a *App) AnswerSecretRequest(id, value string, useSaved, remember bool) error {
	if a.secrets == nil {
		return fmt.Errorf("secret requests not ready")
	}
	return a.secrets.Answer(id, value, useSaved, remember)
}

func (a *App) DeclineSecretRequest(id string) error {
	if a.secrets == nil {
		return fmt.Errorf("secret requests not ready")
	}
	return a.secrets.Decline(id)
}

// ListSavedSecrets: names + dates for a project, never values.
func (a *App) ListSavedSecrets(repoID int64) ([]savedSecret, error) {
	if a.secrets == nil {
		return []savedSecret{}, nil
	}
	return a.secrets.ListSaved(repoID)
}

func (a *App) DeleteSavedSecret(repoID int64, name string) error {
	if a.secrets == nil {
		return fmt.Errorf("secret requests not ready")
	}
	return a.secrets.DeleteSaved(repoID, name)
}
