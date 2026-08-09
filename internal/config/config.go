// Package config holds run configuration and orc's on-disk locations.
package config

import (
	"os"
	"path/filepath"
	"strconv"
)

const DefaultEffort = "medium"

// Version is set from main via ldflags.
var Version = "dev"

type Config struct {
	Provider     string
	Model        string
	Effort       string
	SessionID    string
	Cwd          string // session working directory for tools and instructions
	Instructions string
}

func ExpandHome(path string) string {
	if len(path) > 1 && path[0] == '~' && path[1] == '/' {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

// Home is $XDG_CONFIG_HOME/orc, ~/.config/orc when ~/.config exists, else ~/.orc.
func Home() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" && xdg[0] == '/' {
		return filepath.Join(xdg, "orc")
	}
	cfg := ExpandHome("~/.config")
	if st, err := os.Stat(cfg); err == nil && st.IsDir() {
		return filepath.Join(cfg, "orc")
	}
	return ExpandHome("~/.orc")
}

func Path(rel string) string { return filepath.Join(Home(), rel) }

// WriteFileAtomic writes via a temp file and rename.
func WriteFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp." + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
