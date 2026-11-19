// Package ctlauth is the loopback control API's challenge-response: a client
// proves it holds control.token (or a caller key derived from it) without
// sending it, and the server proves the same back. Whoever holds the port while
// the core is down learns nothing it can replay. Shared by the core (verifier),
// burrow-mcp and the UI process (clients); bin/burrow builds the same strings
// with openssl.
//
//	POST /v1/_challenge                           → {"nonce": "<64 hex>"}
//	Authorization: Burrow-Proof <sn> <cn> <proof> [<caller>]
//	X-Burrow-Proof: <server proof>                 (on the response)
//
// proof = HMAC-SHA256(key, "burrow-ctl-v1 client\n" + sn + "\n" + cn + "\n" + port),
// server proof the same with "server". key = the token, or for <caller>
// ("chat:<id>" / "pty:<id>") CallerKey(token, caller). port = the TCP port the
// client dialed, so a relay to another port fails.
package ctlauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const (
	Scheme      = "Burrow-Proof"
	ProofHeader = "X-Burrow-Proof"
)

func mac(key, msg string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil))
}

// Proof is one side's ("client" / "server") proof for a challenge.
func Proof(key, side, serverNonce, clientNonce string, port int) string {
	return mac(key, "burrow-ctl-v1 "+side+"\n"+serverNonce+"\n"+clientNonce+"\n"+strconv.Itoa(port))
}

// CallerKey is a chat's / tab's own key (BURROW_CALLER_TOKEN): stateless, the
// server recomputes it from the master token and the caller it names.
func CallerKey(master, caller string) string { return mac(master, "caller:"+caller) }

// Equal compares two proofs in constant time.
func Equal(a, b string) bool { return hmac.Equal([]byte(a), []byte(b)) }

// Nonce is 32 random bytes, hex.
func Nonce() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// Parse splits an Authorization value; ok is false for anything but our scheme.
func Parse(h string) (sn, cn, proof, caller string, ok bool) {
	f := strings.Fields(h)
	if len(f) < 4 || len(f) > 5 || f[0] != Scheme {
		return "", "", "", "", false
	}
	if len(f) == 5 {
		caller = f[4]
	}
	return f[1], f[2], f[3], caller, true
}

// Client signs requests to the core at Port with Key (control.token, or a
// caller key with Caller set).
type Client struct {
	Port   int
	Key    string
	Caller string
	HTTP   *http.Client
}

// ErrUnproven: the server did not prove it holds the token. Its reply is not
// to be trusted (something else may hold the port).
var ErrUnproven = errors.New("the server on the Burrow port did not prove it is Burrow (restart Burrow)")

// Do fetches a challenge, signs req, sends it and checks the server's proof
// before handing back the response.
func (c Client) Do(req *http.Request) (*http.Response, error) {
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/_challenge", c.Port), "application/json", nil)
	if err != nil {
		return nil, err
	}
	var ch struct {
		Nonce string `json:"nonce"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&ch)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || err != nil || ch.Nonce == "" {
		return nil, fmt.Errorf("control challenge: %s (core older than this client? restart Burrow)", resp.Status)
	}
	cn := Nonce()
	h := strings.Join([]string{Scheme, ch.Nonce, cn, Proof(c.Key, "client", ch.Nonce, cn, c.Port)}, " ")
	if c.Caller != "" {
		h += " " + c.Caller
	}
	req.Header.Set("Authorization", h)
	resp, err = hc.Do(req)
	if err != nil {
		return nil, err
	}
	if !Equal(resp.Header.Get(ProofHeader), Proof(c.Key, "server", ch.Nonce, cn, c.Port)) {
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, errors.New("control key rejected (restart Burrow to reissue it)")
		}
		return nil, ErrUnproven
	}
	return resp, nil
}
