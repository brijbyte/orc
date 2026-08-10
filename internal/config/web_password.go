package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

const webAuthSection = "web"

type webAuth struct {
	PasswordHash string `json:"password_hash"`
	SessionKey   string `json:"session_key"`
}

// EnsureWebAuth creates web credentials once. Password is set only when created.
func EnsureWebAuth() (password string, created bool, err error) {
	root, err := loadAuthRoot()
	if err != nil {
		return "", false, err
	}
	if raw, ok := root[webAuthSection]; ok {
		var auth webAuth
		if json.Unmarshal(raw, &auth) != nil || auth.PasswordHash == "" || auth.SessionKey == "" {
			return "", false, fmt.Errorf("invalid web credentials in %s", Path("auth.json"))
		}
		return "", false, nil
	}
	password, err = newSecret(24)
	if err != nil {
		return "", false, err
	}
	auth, err := newWebAuth(password)
	if err != nil {
		return "", false, err
	}
	if err := putWebAuth(root, auth); err != nil {
		return "", false, err
	}
	return password, true, nil
}

// VerifyWebPassword compares a password with the stored slow hash.
func VerifyWebPassword(password string) (bool, error) {
	auth, err := loadWebAuth()
	if err != nil {
		return false, err
	}
	err = bcrypt.CompareHashAndPassword([]byte(auth.PasswordHash), []byte(password))
	return err == nil, nil
}

// WebSessionKey returns the secret used to sign session cookies.
func WebSessionKey() (string, error) {
	auth, err := loadWebAuth()
	if err != nil {
		return "", err
	}
	return auth.SessionKey, nil
}

// RotateWebPassword replaces the password hash and cookie signing key.
func RotateWebPassword() (string, error) {
	root, err := loadAuthRoot()
	if err != nil {
		return "", err
	}
	password, err := newSecret(24)
	if err != nil {
		return "", err
	}
	auth, err := newWebAuth(password)
	if err != nil {
		return "", err
	}
	if err := putWebAuth(root, auth); err != nil {
		return "", err
	}
	return password, nil
}

func loadWebAuth() (webAuth, error) {
	root, err := loadAuthRoot()
	if err != nil {
		return webAuth{}, err
	}
	var auth webAuth
	if raw, ok := root[webAuthSection]; !ok || json.Unmarshal(raw, &auth) != nil ||
		auth.PasswordHash == "" || auth.SessionKey == "" {
		return webAuth{}, fmt.Errorf("no web credentials — start `orc --serve`")
	}
	return auth, nil
}

func newWebAuth(password string) (webAuth, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return webAuth{}, err
	}
	key, err := newSecret(32)
	if err != nil {
		return webAuth{}, err
	}
	return webAuth{PasswordHash: string(hash), SessionKey: key}, nil
}

func putWebAuth(root map[string]json.RawMessage, auth webAuth) error {
	raw, _ := json.Marshal(auth)
	root[webAuthSection] = raw
	data, _ := json.MarshalIndent(root, "", "  ")
	if err := os.MkdirAll(Home(), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(Home(), 0o700); err != nil {
		return err
	}
	return WriteFileAtomic(Path("auth.json"), data)
}

func loadAuthRoot() (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(Path("auth.json"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || root == nil {
		return nil, fmt.Errorf("%s is not valid JSON", Path("auth.json"))
	}
	return root, nil
}

func newSecret(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
