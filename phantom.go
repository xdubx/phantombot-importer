package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// bot talks to the PhantomBot web panel websocket, the same API the panel itself uses.
type bot struct {
	base     string // wss://host:port
	insecure bool
	conn     *websocket.Conn
	seq      int
}

func newBot(u string) *bot {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	u = strings.Replace(strings.Replace(u, "https://", "wss://", 1), "http://", "ws://", 1)
	if !strings.Contains(u, "://") {
		u = "wss://" + u
	}
	return &bot{base: u}
}

func isCertError(err error) bool {
	var cv *tls.CertificateVerificationError
	return errors.As(err, &cv)
}

func (b *bot) dial(ctx context.Context, path string) (*websocket.Conn, error) {
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: b.insecure}}}
	// the panel always sends ?target=<host:port>; newer bots use it for origin checks
	target := b.base[strings.Index(b.base, "://")+3:]
	c, _, err := websocket.Dial(ctx, b.base+path+"?target="+url.QueryEscape(target), &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(256 << 20) // dbkeys returns whole tables
	return c, nil
}

// login trades panel credentials for an auth token and opens an authenticated panel session.
func (b *bot) login(ctx context.Context, user, pass string) error {
	c, err := b.dial(ctx, "/ws/panel/login")
	if err != nil {
		return err
	}
	defer c.CloseNow()
	// same request the panel's login page sends: password as SHA-256 hex
	sum := sha256.Sum256([]byte(pass))
	req := map[string]any{"remote": true, "query": "login", "params": map[string]string{"user": user, "pass": hex.EncodeToString(sum[:]), "type": "Auth"}}
	if err := wsjson.Write(ctx, c, req); err != nil {
		return err
	}
	var res struct {
		Authtoken *string `json:"authtoken"`
		Errors    []struct {
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	var raw []byte
	for { // some bot versions answer with an {"authresult":...} frame before the login reply
		_, raw, err = c.Read(ctx)
		if err != nil {
			return err
		}
		if !strings.Contains(string(raw), `"authresult"`) {
			break
		}
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("login: unexpected reply %s", raw)
	}
	if len(res.Errors) > 0 {
		return errors.New(res.Errors[0].Detail)
	}
	if res.Authtoken == nil {
		return fmt.Errorf("login: no auth token in reply %s", raw)
	}

	b.conn, err = b.dial(ctx, "/ws/panel")
	if err != nil {
		return err
	}
	if err := wsjson.Write(ctx, b.conn, map[string]string{"authenticate": *res.Authtoken}); err != nil {
		return err
	}
	var auth struct {
		Authresult string `json:"authresult"`
	}
	if err := wsjson.Read(ctx, b.conn, &auth); err != nil {
		return err
	}
	if auth.Authresult != "true" {
		return errors.New("panel rejected the auth token")
	}
	return nil
}

// call sends one request and waits for the reply carrying its query_id.
func (b *bot) call(op string, body map[string]any) (json.RawMessage, error) {
	b.seq++
	id := "imp" + strconv.Itoa(b.seq)
	body[op] = id
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, b.conn, body); err != nil {
		return nil, err
	}
	for {
		var res struct {
			QueryID string          `json:"query_id"`
			Results json.RawMessage `json:"results"`
		}
		if err := wsjson.Read(ctx, b.conn, &res); err != nil {
			return nil, fmt.Errorf("%s: no reply from bot: %w", op, err)
		}
		if res.QueryID == id {
			return res.Results, nil
		}
		if res.QueryID == "notification" {
			var n struct{ Type, Message string }
			json.Unmarshal(res.Results, &n)
			if n.Type == "permission" || n.Type == "error" {
				return nil, fmt.Errorf("%s: %s", op, n.Message)
			}
		}
		// other broadcasts (panel updates etc.) are ignored
	}
}

// ponytail: one round-trip per write; pipeline the writes if big imports over a slow link take too long.
func (b *bot) set(table, key, value string) error {
	_, err := b.call("dbupdate", map[string]any{"update": map[string]string{"table": table, "key": key, "value": value}})
	return err
}

func (b *bot) incr(table, key string, value int) error {
	_, err := b.call("dbincr", map[string]any{"incr": map[string]string{"table": table, "key": key, "value": strconv.Itoa(value)}})
	return err
}

func (b *bot) keys(table string) (map[string]string, error) {
	raw, err := b.call("dbkeys", map[string]any{"query": map[string]string{"table": table}})
	if err != nil {
		return nil, err
	}
	var rows []struct{ Key, Value string }
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Key] = r.Value
	}
	return out, nil
}
