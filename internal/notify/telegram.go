package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func init() {
	register(Type{
		ID:    "telegram",
		Label: "Telegram",
		Fields: []Field{
			{Key: "token", Label: "bot token", Placeholder: "123456:ABC-DEF…", Secret: true},
			{Key: "chat_id", Label: "chat ID", Placeholder: "123456789"},
		},
		Send: sendTelegram,
	})
}

// Telegram has no priority header, so urgency rides in the title.
var telegramMark = map[string]string{"warn": "⚠️ ", "urgent": "🚨 "}

var telegramAPI = "https://api.telegram.org" // overridden in tests

func sendTelegram(ctx context.Context, settings map[string]string, m Message) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var text strings.Builder
	text.WriteString(telegramMark[m.Urgency])
	text.WriteString(m.Title)
	for _, part := range []string{m.Body, m.URL} {
		if part != "" {
			if text.Len() > 0 {
				text.WriteString("\n\n")
			}
			text.WriteString(part)
		}
	}
	// No parse_mode: Telegram then treats the text as literal, so tool output
	// with stray markup cannot break the message or inject formatting.
	body, _ := json.Marshal(map[string]string{
		"chat_id": settings["chat_id"],
		"text":    text.String(),
	})
	url := telegramAPI + "/bot" + settings["token"] + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram: HTTP %d: %s", resp.StatusCode, telegramError(resp.Body))
	}
	return nil
}

// telegramError picks the API's description out of an error body; the token
// is in the URL, never the body, so nothing secret can come back this way.
func telegramError(r io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(r, 300))
	var api struct {
		Description string `json:"description"`
	}
	if json.Unmarshal(data, &api) == nil && api.Description != "" {
		return api.Description
	}
	return string(data)
}
