package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParsePastedCallback(t *testing.T) {
	for _, test := range []struct {
		pasted string
		code   string
		state  string
	}{
		{"plain-code", "plain-code", ""},
		{"http://localhost:1455/auth/callback?code=abc&state=xyz", "abc", "xyz"},
		{"code=abc&state=xyz", "abc", "xyz"},
	} {
		code, state, err := parsePastedCallback(test.pasted)
		if err != nil || code != test.code || state != test.state {
			t.Errorf("parse %q = %q, %q, %v", test.pasted, code, state, err)
		}
	}
	if _, _, err := parsePastedCallback("http://localhost:1455/auth/callback?error=access_denied"); err == nil {
		t.Fatal("accepted OAuth error callback")
	}
}

func TestWebAuthStatusIncludesExpiry(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	expires := time.Now().Add(time.Hour).Truncate(time.Second)
	payload, _ := json.Marshal(map[string]int64{"exp": expires.Unix()})
	access := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	if err := storePut("codex", map[string]any{
		"tokens": map[string]string{"access_token": access, "account_id": "acct"},
	}); err != nil {
		t.Fatal(err)
	}
	status := new(Codex).WebAuthStatus()
	if !status.Authenticated || status.ExpiresAt != expires.UTC().Format(time.RFC3339) {
		t.Fatalf("status = %#v", status)
	}
}

func TestWebLoginRejectsWrongCallbackState(t *testing.T) {
	p := new(Codex)
	authURL, err := p.BeginWebLogin()
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authURL)
	if err != nil || u.Query().Get("state") == "" || !strings.HasPrefix(authURL, authorizeURL) {
		t.Fatalf("auth URL = %q, %v", authURL, err)
	}
	err = p.CompleteWebLogin(context.Background(), redirectURI+"?code=abc&state=wrong")
	if err == nil || !strings.Contains(err.Error(), "wrong state") {
		t.Fatalf("complete error = %v", err)
	}
}
