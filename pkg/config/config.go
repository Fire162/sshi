package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Fire162/sshi/pkg/sanitize"
	"github.com/Fire162/sshi/pkg/session"
)

// SavedSession represents a bookmarked sshx session.
type SavedSession struct {
	Name       string    `json:"name"`
	Target     string    `json:"target"`
	Host       string    `json:"host"`
	SessionID  string    `json:"sessionId"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
	Notes      string    `json:"notes,omitempty"`
}

// Config stores user preferences and saved sessions.
type Config struct {
	LastSession string                  `json:"lastSession,omitempty"`
	DefaultName string                  `json:"defaultName,omitempty"`
	Sessions    map[string]SavedSession `json:"sessions"`
	configPath  string
}

// DefaultConfigDir returns ~/.config/sshi (or $XDG_CONFIG_HOME/sshi).
func DefaultConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "sshi"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "sshi"), nil
}

// DefaultConfigPath returns ~/.config/sshi/config.json.
func DefaultConfigPath() (string, error) {
	dir, err := DefaultConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// LoadConfig loads the configuration file, creating a default one if not found.
func LoadConfig() (*Config, error) {
	path, err := DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadConfigFromPath(path)
}

// LoadConfigFromPath loads config from an explicit file path.
func LoadConfigFromPath(path string) (*Config, error) {
	cfg := &Config{
		Sessions:   make(map[string]SavedSession),
		configPath: path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}
	if cfg.Sessions == nil {
		cfg.Sessions = make(map[string]SavedSession)
	}
	cfg.configPath = path

	// Enforce strict file permissions if file exists
	_ = os.Chmod(path, 0600)

	return cfg, nil
}

// Save persists the configuration to disk.
func (c *Config) Save() error {
	if c.configPath == "" {
		p, err := DefaultConfigPath()
		if err != nil {
			return err
		}
		c.configPath = p
	}

	dir := filepath.Dir(c.configPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	tmpFile := c.configPath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write temporary config: %w", err)
	}
	if err := os.Rename(tmpFile, c.configPath); err != nil {
		return fmt.Errorf("failed to replace config file: %w", err)
	}

	_ = os.Chmod(c.configPath, 0600)
	return nil
}

// SaveSession bookmarks a session by alias.
func (c *Config) SaveSession(alias string, targetInput string, notes string) (*SavedSession, error) {
	alias = stringsTrim(alias)
	if !sanitize.ValidateAlias(alias) {
		return nil, fmt.Errorf("invalid alias name %q: must contain only letters, numbers, '.', '-', or '_'", alias)
	}

	target, err := session.ParseTarget(targetInput, "")
	if err != nil {
		return nil, fmt.Errorf("invalid session target: %w", err)
	}

	notes = sanitize.CleanSingleLine(notes, 256)

	now := time.Now()
	existing, found := c.Sessions[alias]
	createdAt := now
	if found && !existing.CreatedAt.IsZero() {
		createdAt = existing.CreatedAt
	}

	saved := SavedSession{
		Name:       alias,
		Target:     targetInput,
		Host:       target.Host,
		SessionID:  target.ID,
		CreatedAt:  createdAt,
		LastUsedAt: now,
		Notes:      notes,
	}
	c.Sessions[alias] = saved
	c.LastSession = targetInput
	return &saved, c.Save()
}

// ResolveTarget takes an alias or raw target string, or returns the last session if empty.
func (c *Config) ResolveTarget(input string) (string, error) {
	input = stringsTrim(input)
	if input == "" {
		if c.LastSession != "" {
			return c.LastSession, nil
		}
		return "", fmt.Errorf("no session provided and no previous session in history")
	}

	// 1. Check if input is a saved alias
	if saved, found := c.Sessions[input]; found {
		// Update last used
		saved.LastUsedAt = time.Now()
		c.Sessions[input] = saved
		c.LastSession = saved.Target
		_ = c.Save()
		return saved.Target, nil
	}

	// 2. Validate as a direct session target
	if _, err := session.ParseTarget(input, ""); err == nil {
		c.LastSession = input
		_ = c.Save()
		return input, nil
	}

	return "", fmt.Errorf("unknown session alias or invalid target %q", input)
}

// DeleteSession removes a bookmarked session.
func (c *Config) DeleteSession(alias string) error {
	if _, found := c.Sessions[alias]; !found {
		return fmt.Errorf("session alias %q not found", alias)
	}
	delete(c.Sessions, alias)
	return c.Save()
}

// ListSessions returns all saved sessions sorted by most recently used.
func (c *Config) ListSessions() []SavedSession {
	list := make([]SavedSession, 0, len(c.Sessions))
	for _, s := range c.Sessions {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].LastUsedAt.After(list[j].LastUsedAt)
	})
	return list
}

func stringsTrim(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
