package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

// EnsureDrainToken returns the local service-updater credential. It is separate
// from browser auth and exists only to protect the graceful drain endpoint.
func EnsureDrainToken() (string, error) {
	path := Path("drain.key")
	if data, err := os.ReadFile(path); err == nil {
		if token := strings.TrimSpace(string(data)); token != "" {
			return token, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(key[:])
	if err := WriteFileAtomic(path, []byte(token+"\n")); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return token, nil
}
