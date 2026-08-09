package codex

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/brijbyte/orc/internal/config"
)

const (
	authBase      = "https://auth.openai.com"
	authorizeURL  = authBase + "/oauth/authorize"
	tokenURL      = authBase + "/oauth/token"
	clientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	refreshWindow = 5 * time.Minute
	staleAfter    = 8 * 24 * time.Hour
)

// authFile is orc's loaded credential store: sec is the codex section inside
// root (the section itself keeps the Codex CLI token schema).
type authFile struct {
	root map[string]json.RawMessage
	sec  map[string]json.RawMessage
	path string
}

func parseObject(data []byte) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

// loadAuthFile reads orc's provider-keyed store.
func loadAuthFile() (*authFile, error) {
	path := config.Path("auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no credentials — run `orc --login`")
	}
	root := parseObject(data)
	if root == nil {
		return nil, fmt.Errorf("%s is not valid JSON", path)
	}
	af := &authFile{root: root, path: path}
	if sec, ok := root["codex"]; ok {
		af.sec = parseObject(sec)
	} else if _, wasFlat := root["tokens"]; wasFlat {
		// migrate the flat pre-store layout written by early orc logins
		af.sec = root
		af.root = map[string]json.RawMessage{}
		af.write()
	}
	if af.sec == nil {
		return nil, fmt.Errorf("no codex credentials in %s — run `orc --login`", path)
	}
	return af, nil
}

func (af *authFile) write() error {
	sec, _ := json.Marshal(af.sec)
	af.root["codex"] = sec
	out, _ := json.MarshalIndent(af.root, "", "  ")
	return config.WriteFileAtomic(af.path, out)
}

func (af *authFile) str(key string) string {
	var s string
	json.Unmarshal(af.sec[key], &s)
	return s
}

// tokens returns the string fields of the tokens object.
func (af *authFile) tokens() map[string]string {
	var raw map[string]json.RawMessage
	json.Unmarshal(af.sec["tokens"], &raw)
	t := map[string]string{}
	for k, v := range raw {
		var s string
		if json.Unmarshal(v, &s) == nil {
			t[k] = s
		}
	}
	return t
}

func (af *authFile) setTokens(t map[string]string) {
	raw, _ := json.Marshal(t)
	af.sec["tokens"] = raw
}

func (af *authFile) setStr(key, value string) {
	raw, _ := json.Marshal(value)
	af.sec[key] = raw
}

// jwtClaims decodes a JWT payload without verifying, or nil.
func jwtClaims(jwt string) map[string]any {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return nil
	}
	return claims
}

func jwtExp(jwt string) int64 {
	claims := jwtClaims(jwt)
	exp, _ := claims["exp"].(float64)
	return int64(exp)
}

func nowRFC3339() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func (af *authFile) needsRefresh() bool {
	if at := af.tokens()["access_token"]; at != "" {
		if exp := jwtExp(at); exp != 0 && time.Until(time.Unix(exp, 0)) < refreshWindow {
			return true
		}
	}
	if lr := af.str("last_refresh"); lr != "" {
		if ts, err := time.Parse(time.RFC3339, lr); err == nil &&
			time.Since(ts) > staleAfter {
			return true
		}
	}
	return false
}

// refresh rotates tokens in place and rewrites the auth file.
func (af *authFile) refresh(notify func(string)) error {
	tokens := af.tokens()
	rt := tokens["refresh_token"]
	if rt == "" {
		return fmt.Errorf("no refresh_token in %s", af.path)
	}
	body, _ := json.Marshal(map[string]string{
		"client_id":     clientID,
		"grant_type":    "refresh_token",
		"refresh_token": rt,
	})
	resp, status, err := httpPost(tokenURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("token refresh failed: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("token refresh failed (HTTP %d): %.300s", status, resp)
	}
	var next map[string]string
	if json.Unmarshal(resp, &next) != nil {
		return fmt.Errorf("token refresh: bad response JSON")
	}
	for _, key := range []string{"access_token", "id_token", "refresh_token"} {
		if next[key] != "" {
			tokens[key] = next[key]
		}
	}
	af.setTokens(tokens)
	af.setStr("last_refresh", nowRFC3339())
	if err := af.write(); err != nil {
		return fmt.Errorf("failed to write %s", af.path)
	}
	notify("✅ orc: refreshed tokens")
	return nil
}

// loadAuth returns a valid access token and account id, refreshing if due.
func loadAuth(notify func(string)) (accessToken, accountID string, err error) {
	af, err := loadAuthFile()
	if err != nil {
		return "", "", err
	}
	if af.needsRefresh() {
		// Re-read right before refreshing: another process may have rotated.
		if af, err = loadAuthFile(); err != nil {
			return "", "", err
		}
		if af.needsRefresh() {
			if err := af.refresh(notify); err != nil {
				return "", "", err
			}
		}
	}
	tokens := af.tokens()
	at, acct := tokens["access_token"], tokens["account_id"]
	if at == "" || acct == "" {
		return "", "", fmt.Errorf("%s missing access_token/account_id", af.path)
	}
	return at, acct, nil
}

func (p *Codex) Authenticated() bool {
	af, err := loadAuthFile()
	if err != nil {
		return false
	}
	tokens := af.tokens()
	return tokens["access_token"] != "" && tokens["account_id"] != ""
}

func (p *Codex) AuthStatus() error {
	af, err := loadAuthFile()
	if err != nil {
		return err
	}
	tokens := af.tokens()
	orNone := func(s string) string {
		if s == "" {
			return "(none)"
		}
		return s
	}
	fmt.Printf("🤖 provider:     codex\n")
	fmt.Printf("📄 auth_file:    %s\n", af.path)
	fmt.Printf("🔐 auth_mode:    %s\n", orNone(af.str("auth_mode")))
	fmt.Printf("🪪 account_id:   %s\n", orNone(tokens["account_id"]))
	if at := tokens["access_token"]; at != "" {
		if exp := jwtExp(at); exp != 0 {
			state := "valid"
			if exp <= time.Now().Unix() {
				state = "EXPIRED"
			}
			fmt.Printf("🔑 token:        %s (expires in %d min)\n", state,
				(exp-time.Now().Unix())/60)
		} else {
			fmt.Printf("🔑 token:        present (no exp claim)\n")
		}
	} else {
		fmt.Printf("🔑 token:        MISSING\n")
	}
	fmt.Printf("🕒 last_refresh: %s\n", orNone(af.str("last_refresh")))
	due := "no"
	if af.needsRefresh() {
		due = "yes"
	}
	fmt.Printf("🔄 refresh due:  %s\n", due)
	if tokens["access_token"] == "" || tokens["account_id"] == "" {
		return fmt.Errorf("not authenticated")
	}
	return nil
}

// storePut writes a provider section into orc's auth store.
func storePut(providerName string, section any) error {
	root := map[string]json.RawMessage{}
	if data, err := os.ReadFile(config.Path("auth.json")); err == nil {
		if m := parseObject(data); m != nil {
			// drop legacy flat layout (pre provider-keyed store)
			if _, wasFlat := m["tokens"]; !wasFlat {
				root = m
			}
		}
	}
	sec, err := json.Marshal(section)
	if err != nil {
		return err
	}
	root[providerName] = sec
	if err := os.MkdirAll(config.Home(), 0o755); err != nil {
		return err
	}
	out, _ := json.MarshalIndent(root, "", "  ")
	return config.WriteFileAtomic(config.Path("auth.json"), out)
}
