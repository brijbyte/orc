// Package codex implements the OpenAI Codex backend: ChatGPT-subscription
// auth (orc's own OAuth login with token refresh) + Responses-API over SSE.
package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/brijbyte/orc/internal/config"
	"github.com/brijbyte/orc/internal/provider"
	"github.com/brijbyte/orc/internal/provider/modelsdev"
)

const (
	codexURL   = "https://chatgpt.com/backend-api/codex/responses"
	originator = "orc"
)

type Codex struct {
	webLoginMu sync.Mutex
	webLogin   *webLoginAttempt
}

func init() { provider.Register(&Codex{}) }

func (p *Codex) Name() string         { return "codex" }
func (p *Codex) DefaultModel() string { return "gpt-5.6-sol" }

func httpPost(url, contentType string, body io.Reader) ([]byte, int, error) {
	resp, err := http.Post(url, contentType, body)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

type turnRequest struct {
	Model             string            `json:"model"`
	Instructions      string            `json:"instructions"`
	Input             []json.RawMessage `json:"input"`
	Tools             json.RawMessage   `json:"tools,omitempty"`
	ToolChoice        string            `json:"tool_choice"`
	ParallelToolCalls bool              `json:"parallel_tool_calls"`
	Reasoning         struct {
		Effort  string `json:"effort"`
		Summary string `json:"summary"`
	} `json:"reasoning"`
	Store          bool     `json:"store"`
	Stream         bool     `json:"stream"`
	Include        []string `json:"include"`
	PromptCacheKey string   `json:"prompt_cache_key"`
}

type sseEvent struct {
	Type     string          `json:"type"`
	Delta    json.RawMessage `json:"delta"`
	Item     json.RawMessage `json:"item"`
	Response *struct {
		Usage *struct {
			TotalTokens int64 `json:"total_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"response"`
}

// streamState buffers completed output items; they are forwarded to the agent
// only after the whole stream succeeds, so a mid-stream retry cannot
// duplicate them.
type streamState struct {
	cb      *provider.Callbacks
	items   []json.RawMessage
	failed  string
	retry   bool // the failure was server-side, so another attempt can help
	thought bool // a reasoning summary part already streamed
}

func (st *streamState) onEvent(data []byte) {
	var ev sseEvent
	if json.Unmarshal(data, &ev) != nil {
		return
	}
	switch ev.Type {
	case "response.output_text.delta":
		var s string
		if json.Unmarshal(ev.Delta, &s) == nil && st.cb.OnTextDelta != nil {
			st.cb.OnTextDelta(s)
		}
	case "response.reasoning_summary_part.added":
		// summary parts arrive back to back: break them apart
		if st.thought && st.cb.OnThinkingDelta != nil {
			st.cb.OnThinkingDelta("\n\n")
		}
	case "response.reasoning_summary_text.delta":
		var s string
		if json.Unmarshal(ev.Delta, &s) == nil && st.cb.OnThinkingDelta != nil {
			st.thought = true
			st.cb.OnThinkingDelta(s)
		}
	case "response.output_item.done":
		if len(ev.Item) > 0 {
			st.items = append(st.items, ev.Item)
		}
	case "response.completed":
		if ev.Response != nil && ev.Response.Usage != nil && st.cb.OnUsage != nil {
			st.cb.OnUsage(ev.Response.Usage.TotalTokens)
		}
	case "response.failed", "response.incomplete":
		// A 200 stream can still end in failure (overload, internal error).
		// "incomplete" is a limit the same request would hit again.
		st.failed = ev.Type
		st.retry = ev.Type == "response.failed"
		if ev.Response != nil && ev.Response.Error != nil && ev.Response.Error.Message != "" {
			st.failed = ev.Response.Error.Message
		}
	}
	// output_item.added, content_part.*, etc.: ignored.
}

// readSSE dispatches each SSE data payload to st. Returns on stream end.
func readSSE(body io.Reader, st *streamState) error {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var data []byte
	for sc.Scan() {
		line := sc.Bytes()
		switch {
		case len(line) == 0:
			if len(data) > 0 {
				st.onEvent(data)
				data = nil
			}
		case bytes.HasPrefix(line, []byte("data:")):
			payload := bytes.TrimPrefix(line, []byte("data:"))
			payload = bytes.TrimPrefix(payload, []byte(" "))
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, payload...)
		}
	}
	if len(data) > 0 {
		st.onEvent(data)
	}
	return sc.Err()
}

func (p *Codex) Turn(ctx context.Context, history []json.RawMessage,
	tools json.RawMessage, cfg *config.Config, cb *provider.Callbacks) error {
	notify := cb.OnNotice
	if notify == nil {
		notify = func(string) {}
	}
	accessToken, accountID, err := loadAuth(notify)
	if err != nil {
		return err
	}

	req := turnRequest{
		Model:             cfg.Model,
		Instructions:      cfg.Instructions,
		Input:             history,
		Tools:             tools,
		ToolChoice:        "auto",
		ParallelToolCalls: true,
		Store:             false,
		Stream:            true,
		Include:           []string{"reasoning.encrypted_content"},
		PromptCacheKey:    cfg.SessionID,
	}
	req.Reasoning.Effort = cfg.Effort
	req.Reasoning.Summary = "auto"
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	const attempts = 4
	for attempt := 0; ; attempt++ {
		st := &streamState{cb: cb}
		status, err := p.once(ctx, body, accessToken, accountID, cfg, st)

		if ctx.Err() != nil {
			return provider.ErrInterrupted
		}
		var last error
		switch {
		case err != nil:
			last = fmt.Errorf("connection dropped: %w", err)
		case status < 200 || status >= 300:
			last = fmt.Errorf("HTTP %d: %.500s", status, st.failed)
		case st.failed != "":
			last = fmt.Errorf("model error: %.500s", st.failed)
		default:
			for _, item := range st.items {
				if cb.OnItemDone != nil {
					cb.OnItemDone(item)
				}
			}
			return nil
		}

		if !(err != nil || status == 429 || status >= 500 || st.retry) {
			return last
		}
		if attempt == attempts-1 {
			return fmt.Errorf("%w (%d attempts)", last, attempts)
		}
		// The response regenerates from scratch; buffered items are dropped.
		wait := 2 << attempt
		notify(fmt.Sprintf("🔄 orc: %v — retrying in %ds...", last, wait))
		select {
		case <-time.After(time.Duration(wait) * time.Second):
		case <-ctx.Done():
			return provider.ErrInterrupted
		}
	}
}

// once runs a single streaming request. Non-2xx bodies land in st.failed.
func (p *Codex) once(ctx context.Context, body []byte, accessToken,
	accountID string, cfg *config.Config, st *streamState) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", codexURL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("chatgpt-account-id", accountID)
	req.Header.Set("session_id", cfg.SessionID)
	req.Header.Set("x-client-request-id", cfg.SessionID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("originator", originator)
	req.Header.Set("User-Agent", "orc/"+config.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		st.failed = string(msg)
		return resp.StatusCode, nil
	}
	if err := readSSE(resp.Body, st); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

// Models returns selectable models from models.dev; best effort.
func (p *Codex) Models() []provider.Model {
	return modelsdev.Models("openai")
}
