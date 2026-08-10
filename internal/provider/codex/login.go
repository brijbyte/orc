package codex

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/brijbyte/orc/internal/config"
)

const (
	loginPort   = 1455 // the redirect URI registered for the client
	redirectURI = "http://localhost:1455/auth/callback"
)

const successHTML = `<html><body style="font-family:sans-serif;margin:4em">` +
	`<h2>orc: login successful</h2><p>You can close this tab.</p></body></html>`
const failHTML = `<html><body style="font-family:sans-serif;margin:4em">` +
	`<h2>orc: login failed</h2><p>Go back to the terminal.</p></body></html>`

func randB64(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func openBrowser(u string) {
	cmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		cmd = "open"
	}
	// URL is printed; manual open still works if this fails.
	exec.Command(cmd, u).Start()
}

func (p *Codex) Login(ctx context.Context, notify func(string)) error {
	verifier := randB64(32) // 43 chars, PKCE-valid
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := randB64(16)

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", loginPort))
	if err != nil {
		return fmt.Errorf("cannot listen on localhost:%d (port in use?)", loginPort)
	}

	q := url.Values{
		"response_type":              {"code"},
		"client_id":                  {clientID},
		"redirect_uri":               {redirectURI},
		"scope":                      {"openid profile email offline_access"},
		"code_challenge":             {challenge},
		"code_challenge_method":      {"S256"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"state":                      {state},
	}
	authURL := authorizeURL + "?" + q.Encode()
	notify(fmt.Sprintf("🔐 Open this URL to sign in with ChatGPT:\n\n%s\n\n"+
		"🌐 Waiting for the browser callback on localhost:%d (Ctrl-C to cancel)...",
		authURL, loginPort))
	openBrowser(authURL)

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	server := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/auth/callback") {
				w.WriteHeader(http.StatusNotFound) // favicon and stray requests
				return
			}
			code := r.URL.Query().Get("code")
			cbState := r.URL.Query().Get("state")
			cbErr := r.URL.Query().Get("error")
			w.Header().Set("Content-Type", "text/html")
			if code == "" || cbState != state {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, failHTML)
				reason := cbErr
				if reason == "" {
					reason = "missing code or bad state"
				}
				done <- result{err: fmt.Errorf("login callback rejected (%s)", reason)}
				return
			}
			fmt.Fprint(w, successHTML)
			done <- result{code: code}
		}),
	}
	go server.Serve(listener)
	var res result
	select {
	case res = <-done:
	case <-ctx.Done():
		server.Close()
		return fmt.Errorf("login canceled")
	}
	server.Close()
	if res.err != nil {
		return res.err
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {res.code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}
	resp, status, err := httpPost(tokenURL, "application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()))
	if err != nil || status != 200 {
		return fmt.Errorf("code exchange failed (HTTP %d): %.300s", status, resp)
	}
	var grant struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if json.Unmarshal(resp, &grant) != nil {
		return fmt.Errorf("code exchange: bad response JSON")
	}
	return saveAuth(grant.IDToken, grant.AccessToken, grant.RefreshToken, notify)
}

func saveAuth(idToken, accessToken, refreshToken string, notify func(string)) error {
	if idToken == "" || accessToken == "" || refreshToken == "" {
		return fmt.Errorf("token response missing fields")
	}
	claims := jwtClaims(idToken)
	authClaim, _ := claims["https://api.openai.com/auth"].(map[string]any)
	acct, _ := authClaim["chatgpt_account_id"].(string)
	if acct == "" {
		return fmt.Errorf("id_token has no chatgpt_account_id")
	}
	section := map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]string{
			"id_token":      idToken,
			"access_token":  accessToken,
			"refresh_token": refreshToken,
			"account_id":    acct,
		},
		"last_refresh": nowRFC3339(),
	}
	if err := storePut("codex", section); err != nil {
		return fmt.Errorf("failed to write %s", config.Path("auth.json"))
	}
	notify("✅ orc: logged in; credentials saved to " + config.Path("auth.json"))
	return nil
}
