// Package cli is the homedash binary on a workstation: the hub's API as
// subcommands, signed in as a person through the browser hand-off in
// login.go. It adds nothing the panel cannot do; every write goes through
// the handlers the panel uses, so the gate and the guard apply unchanged.
package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config is ~/.config/homedash/hub.json: which hub, the session it left
// there, and who that session is.
type Config struct {
	Hub   string `json:"hub"`
	Token string `json:"token"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

func configPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "homedash", "hub.json")
}

// Load reads the file. HOMEDASH_HUB and HOMEDASH_TOKEN override it, for a
// script holding an API token instead of a person's session.
func Load() (*Config, error) {
	c := &Config{}
	if b, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(b, c)
	}
	if v := os.Getenv("HOMEDASH_HUB"); v != "" {
		c.Hub = v
	}
	if v := os.Getenv("HOMEDASH_TOKEN"); v != "" {
		c.Token, c.Name, c.Role = v, "token", ""
	}
	if c.Hub == "" || c.Token == "" {
		return nil, errors.New("not signed in; run: homedash login <hub-url>")
	}
	return c, nil
}

// Save writes the file, readable by this user only.
func (c *Config) Save() error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// Forget removes the file; signing out.
func Forget() error {
	err := os.Remove(configPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
