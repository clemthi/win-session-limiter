// Package config loads and hot-reloads the service's config.json.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	DefaultPollIntervalSeconds = 15
	FileName                   = "config.json"

	// MinDailyLimitMinutes is a hard floor on every user's daily limit: no
	// matter what config.json says, a monitored account always gets at
	// least this much logged-on time per day.
	MinDailyLimitMinutes = 30

	// MandatoryWarningMinutes is always included in the warning schedule,
	// regardless of config.json, so a user is never logged off without at
	// least a 5-minute notice.
	MandatoryWarningMinutes = 5
)

var DefaultWarningsMinutesBefore = []int{15, 10, 5, 1}

type UserConfig struct {
	DailyLimitMinutes int `json:"dailyLimitMinutes"`
}

type Config struct {
	PollIntervalSeconds   int                   `json:"pollIntervalSeconds"`
	WarningsMinutesBefore []int                 `json:"warningsMinutesBefore"`
	Users                 map[string]UserConfig `json:"users"`
}

func Default() *Config {
	c := &Config{
		PollIntervalSeconds:   DefaultPollIntervalSeconds,
		WarningsMinutesBefore: append([]int(nil), DefaultWarningsMinutesBefore...),
		Users:                 map[string]UserConfig{},
	}
	c.normalize()
	return c
}

// normalize fills in defaults and enforces the invariants every loaded
// config must satisfy: a sane poll interval, a mandatory 5-minute warning,
// and a 30-minute floor on every user's daily limit.
func (c *Config) normalize() {
	if c.PollIntervalSeconds <= 0 {
		c.PollIntervalSeconds = DefaultPollIntervalSeconds
	}
	if len(c.WarningsMinutesBefore) == 0 {
		c.WarningsMinutesBefore = append([]int(nil), DefaultWarningsMinutesBefore...)
	}
	if !containsInt(c.WarningsMinutesBefore, MandatoryWarningMinutes) {
		c.WarningsMinutesBefore = append(c.WarningsMinutesBefore, MandatoryWarningMinutes)
	}
	if c.Users == nil {
		c.Users = map[string]UserConfig{}
	}
	for name, uc := range c.Users {
		if uc.DailyLimitMinutes < MinDailyLimitMinutes {
			uc.DailyLimitMinutes = MinDailyLimitMinutes
			c.Users[name] = uc
		}
	}
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func (c *Config) PollInterval() time.Duration {
	if c.PollIntervalSeconds <= 0 {
		return DefaultPollIntervalSeconds * time.Second
	}
	return time.Duration(c.PollIntervalSeconds) * time.Second
}

// Lookup returns the configured limit for username (case-insensitive) and
// whether that user is monitored at all.
func (c *Config) Lookup(username string) (UserConfig, bool) {
	for name, uc := range c.Users {
		if strings.EqualFold(name, username) {
			return uc, true
		}
	}
	return UserConfig{}, false
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	c.normalize()
	return &c, nil
}

// Save writes c to path as indented JSON, atomically (temp file + rename).
func Save(path string, c *Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Watcher reloads the config from disk whenever its mtime changes.
type Watcher struct {
	path string

	mu      sync.RWMutex
	current *Config
	modTime time.Time
}

func NewWatcher(path string) (*Watcher, error) {
	w := &Watcher{path: path}
	if err := w.reload(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Watcher) reload() error {
	info, err := os.Stat(w.path)
	if err != nil {
		return err
	}
	c, err := Load(w.path)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.current = c
	w.modTime = info.ModTime()
	w.mu.Unlock()
	return nil
}

// Refresh checks the file's mtime and reloads if it changed. Errors are
// returned but the previously loaded config remains in effect.
func (w *Watcher) Refresh() error {
	info, err := os.Stat(w.path)
	if err != nil {
		return err
	}
	w.mu.RLock()
	unchanged := info.ModTime().Equal(w.modTime)
	w.mu.RUnlock()
	if unchanged {
		return nil
	}
	return w.reload()
}

func (w *Watcher) Current() *Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.current
}
