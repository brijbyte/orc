// Package config holds run configuration and orc's on-disk locations.
package config

import (
	"os"
	"path/filepath"
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
	Routine      string // standing mission; non-empty enables routine tools
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

// WriteFileAtomic writes via a private temp file and rename.
func WriteFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
