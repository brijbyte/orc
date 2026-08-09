package config

import (
	"encoding/json"
	"os"
)

// Settings are user defaults persisted at <orc home>/config.json.
type Settings struct {
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

func LoadSettings() Settings {
	var s Settings
	if data, err := os.ReadFile(Path("config.json")); err == nil {
		json.Unmarshal(data, &s)
	}
	return s
}

func SaveSettings(s Settings) error {
	if err := os.MkdirAll(Home(), 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	return WriteFileAtomic(Path("config.json"), data)
}
