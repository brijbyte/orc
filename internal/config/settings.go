package config

import (
	"encoding/json"
	"os"
)

// Settings are user defaults persisted at <orc home>/config.json.
type Settings struct {
	Model  string          `json:"model,omitempty"`
	Effort string          `json:"effort,omitempty"`
	Pinned []string        `json:"pinned,omitempty"` // session ids kept at the top
	Notify []NotifyChannel `json:"notify,omitempty"`
}

// NotifyChannel is one user-configured notification channel.
type NotifyChannel struct {
	Type     string            `json:"type"` // channel provider id, e.g. "ntfy"
	Name     string            `json:"name"`
	Enabled  bool              `json:"enabled"`
	Settings map[string]string `json:"settings,omitempty"`
}

func LoadSettings() Settings {
	var s Settings
	if data, err := os.ReadFile(Path("config.json")); err == nil {
		json.Unmarshal(data, &s)
	}
	return s
}

func SaveSettings(s Settings) error {
	if err := os.MkdirAll(Home(), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(Home(), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	return WriteFileAtomic(Path("config.json"), data)
}
