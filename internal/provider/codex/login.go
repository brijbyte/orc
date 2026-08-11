package codex

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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
	loginPort       = 1455 // the redirect URI registered for the client
	redirectURI     = "http://localhost:1455/auth/callback"
	webLoginTimeout = 10 * time.Minute
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

type webLoginAttempt struct {
	verifier string
	state    string
	expires  time.Time
}

func newLoginAttempt() (*webLoginAttempt, string) {
	attempt := &webLoginAttempt{
		verifier: randB64(32), // 43 chars, PKCE-valid
		state:    randB64(16),
		expires:  time.Now().Add(webLoginTimeout),
	}
	sum := sha256.Sum256([]byte(attempt.verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	q := url.Values{
		"response_type":              {"code"},
		"client_id":                  {clientID},
		"redirect_uri":               {redirectURI},
		"scope":                      {"openid profile email offline_access"},
		"code_challenge":             {challenge},
		"code_challenge_method":      {"S256"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"state":                      {attempt.state},
	}
	return attempt, authorizeURL + "?" + q.Encode()
}

func (p *Codex) Login(ctx context.Context, notify func(string)) error {
	attempt, authURL := newLoginAttempt()
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", loginPort))
	if err != nil {
		return fmt.Errorf("cannot listen on localhost:%d (port in use?)", loginPort)
	}

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
			if code == "" || cbState != attempt.state {
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

	return exchangeLoginCode(ctx, attempt, res.code, notify)
}

// BeginWebLogin starts a PKCE flow without opening a listener. The callback is
// pasted back into the web UI because its localhost belongs to the browser.
func (p *Codex) BeginWebLogin() (string, error) {
	attempt, authURL := newLoginAttempt()
	p.webLoginMu.Lock()
	p.webLogin = attempt
	p.webLoginMu.Unlock()
	return authURL, nil
}

// CompleteWebLogin accepts either the full localhost callback URL or its code.
func (p *Codex) CompleteWebLogin(ctx context.Context, pasted string) error {
	code, state, err := parsePastedCallback(pasted)
	if err != nil {
		return err
	}
	p.webLoginMu.Lock()
	attempt := p.webLogin
	p.webLogin = nil
	p.webLoginMu.Unlock()
	if attempt == nil || time.Now().After(attempt.expires) {
		return fmt.Errorf("sign-in attempt expired; start again")
	}
	if state != "" && state != attempt.state {
		return fmt.Errorf("sign-in callback has the wrong state")
	}
	return exchangeLoginCode(ctx, attempt, code, func(string) {})
}

func parsePastedCallback(pasted string) (code, state string, err error) {
	pasted = strings.TrimSpace(pasted)
	if pasted == "" {
		return "", "", fmt.Errorf("paste the callback URL or code")
	}
	values := url.Values{}
	if u, parseErr := url.Parse(pasted); parseErr == nil && u.RawQuery != "" {
		values = u.Query()
	} else if strings.Contains(pasted, "code=") {
		values, _ = url.ParseQuery(strings.TrimPrefix(pasted, "?"))
	}
	if oauthErr := values.Get("error"); oauthErr != "" {
		return "", "", fmt.Errorf("sign-in failed: %s", oauthErr)
	}
	if code = values.Get("code"); code == "" {
		code = pasted
	}
	if len(code) > 16<<10 {
		return "", "", fmt.Errorf("authorization code is too long")
	}
	return code, values.Get("state"), nil
}

func exchangeLoginCode(ctx context.Context, attempt *webLoginAttempt, code string,
	notify func(string)) error {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"code_verifier": {attempt.verifier},
		"redirect_uri":  {redirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("code exchange failed: %w", err)
	}
	defer response.Body.Close()
	resp, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != http.StatusOK {
		return fmt.Errorf("code exchange failed (HTTP %d): %.300s", response.StatusCode, resp)
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
	authMu.Lock()
	defer authMu.Unlock()
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
