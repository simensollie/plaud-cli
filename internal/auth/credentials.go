// Package auth handles persistence of the bearer token and login metadata
// the CLI obtained from a Plaud login flow. It does not perform login itself
// (that lives in internal/api); it only stores and retrieves the result.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Credentials is the on-disk shape under
// ${XDG_CONFIG_HOME:-~/.config}/plaud/credentials.json on POSIX and
// %APPDATA%\plaud\credentials.json on Windows.
//
// No password is ever stored. The Token is the long-lived bearer JWT
// returned by Plaud's otp-login (or pasted via plaud login --token).
type Credentials struct {
	Token      string    `json:"token"`
	Region     string    `json:"region"`
	Email      string    `json:"email"`
	ObtainedAt time.Time `json:"obtained_at"`
}

// ErrNotLoggedIn is returned by Load when no credentials file exists. The
// CLI maps this to the "Not logged in. Run `plaud login` first." message.
var ErrNotLoggedIn = errors.New("not logged in")

// defaultPath resolves the credentials file path for the current OS,
// honoring XDG_CONFIG_HOME on POSIX and APPDATA on Windows.
func defaultPath() (string, error) {
	dir, err := defaultConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

func defaultConfigDir() (string, error) {
	if runtime.GOOS == "windows" {
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return "", errors.New("APPDATA environment variable is not set")
		}
		return filepath.Join(appdata, "plaud"), nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "plaud"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".config", "plaud"), nil
}

// Save writes the credentials to disk atomically with mode 0600 on POSIX.
// Fails fast on path or marshal errors; never includes the token in error
// strings.
func Save(c Credentials) error {
	path, err := defaultPath()
	if err != nil {
		return fmt.Errorf("resolving credentials path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		// Marshal can include field values in errors via reflection. To be
		// safe we deliberately do not %w-wrap here; we discard err's text.
		return errors.New("marshaling credentials")
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing credentials: %w", err)
	}
	// Defeat umask: WriteFile passes the mode through it. Force 0600.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmp, 0o600); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("setting credentials mode: %w", err)
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming credentials into place: %w", err)
	}
	return nil
}

// Load reads the credentials file. Returns ErrNotLoggedIn if it does not
// exist. Never includes the token in error strings.
func Load() (Credentials, error) {
	path, err := defaultPath()
	if err != nil {
		return Credentials{}, fmt.Errorf("resolving credentials path: %w", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credentials{}, ErrNotLoggedIn
		}
		return Credentials{}, fmt.Errorf("reading credentials: %w", err)
	}

	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		// Don't %w-wrap: json's syntax errors can include surrounding bytes
		// from the input, which may contain the token. Return a generic
		// message instead.
		return Credentials{}, errors.New("decoding credentials: invalid JSON")
	}
	return c, nil
}

// Delete removes the credentials file. No-op (returns nil) if the file does
// not exist, so logout is idempotent.
func Delete() error {
	path, err := defaultPath()
	if err != nil {
		return fmt.Errorf("resolving credentials path: %w", err)
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("removing credentials: %w", err)
	}
	return nil
}
