package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func init() {
	register(Type{
		ID:    "ntfy",
		Label: "ntfy",
		Fields: []Field{
			{Key: "url", Label: "topic URL", Placeholder: "https://ntfy.sh/my-topic"},
			{Key: "token", Label: "access token", Secret: true, Optional: true},
		},
		Send: sendNtfy,
	})
}

var ntfyPriority = map[string]string{"info": "3", "warn": "4", "urgent": "5"}

func sendNtfy(ctx context.Context, settings map[string]string, m Message) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", settings["url"],
		strings.NewReader(m.Body))
	if err != nil {
		return err
	}
	req.Header.Set("Title", m.Title)
	if p := ntfyPriority[m.Urgency]; p != "" {
		req.Header.Set("Priority", p)
	}
	if m.URL != "" {
		req.Header.Set("Click", m.URL)
	}
	if token := settings["token"]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("ntfy: HTTP %d: %s", resp.StatusCode, body)
	}
	return nil
}
