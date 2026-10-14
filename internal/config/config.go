// Package config loads and hot-reloads the service's config.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultPollIntervalSeconds = 15
	FileName                   = "config.json"

	// MinDailyLimitMinutes is a hard floor on every user's daily limit: no
	// matter what config.json or the add/edit commands say, a monitored
	// account always gets at least this much logged-on time per day. This
	// is the single source of truth for the minimum; don't hardcode it
	// elsewhere.
	MinDailyLimitMinutes = 5
)

var DefaultWarningsMinutesBefore = []int{15, 10, 5, 3, 1}

// MandatoryWarningsMinutes are always included in the warning schedule,
// regardless of config.json. The 3-minute one matters for users at the
// minimum limit: they start with exactly 5 minutes, so the 5-minute warning
// is already behind them and suppressed at logon.
var MandatoryWarningsMinutes = []int{5, 3}

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
// config must satisfy: a sane poll interval, the MandatoryWarningsMinutes
// warnings, and the MinDailyLimitMinutes floor on every user's daily limit.
func (c *Config) normalize() {
	if c.PollIntervalSeconds <= 0 {
		c.PollIntervalSeconds = DefaultPollIntervalSeconds
	}
	if len(c.WarningsMinutesBefore) == 0 {
		c.WarningsMinutesBefore = append([]int(nil), DefaultWarningsMinutesBefore...)
	}
	for _, m := range MandatoryWarningsMinutes {
		if !containsInt(c.WarningsMinutesBefore, m) {
			c.WarningsMinutesBefore = append(c.WarningsMinutesBefore, m)
		}
	}
	if c.Users == nil {
		c.Users = map[string]UserConfig{}
	}
	for name, uc := range c.Users {
		if clamped, raised := ClampLimit(uc.DailyLimitMinutes); raised {
			uc.DailyLimitMinutes = clamped
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

var (
	ErrUserExists   = errors.New("user is already monitored")
	ErrUserNotFound = errors.New("user is not monitored")
)

// Lookup returns the configured limit for username (case-insensitive) and
// whether that user is monitored at all.
func (c *Config) Lookup(username string) (UserConfig, bool) {
	key, ok := c.findKey(username)
	if !ok {
		return UserConfig{}, false
	}
	return c.Users[key], true
}

func (c *Config) findKey(username string) (string, bool) {
	for name := range c.Users {
		if strings.EqualFold(name, username) {
			return name, true
		}
	}
	return "", false
}

func (c *Config) AddUser(username string, minutes int) error {
	if _, ok := c.findKey(username); ok {
		return fmt.Errorf("%q: %w", username, ErrUserExists)
	}
	c.Users[username] = UserConfig{DailyLimitMinutes: minutes}
	return nil
}

// EditUser updates the limit of an existing user, keeping the casing of the
// existing key.
func (c *Config) EditUser(username string, minutes int) error {
	key, ok := c.findKey(username)
	if !ok {
		return fmt.Errorf("%q: %w", username, ErrUserNotFound)
	}
	c.Users[key] = UserConfig{DailyLimitMinutes: minutes}
	return nil
}

func (c *Config) DeleteUser(username string) error {
	key, ok := c.findKey(username)
	if !ok {
		return fmt.Errorf("%q: %w", username, ErrUserNotFound)
	}
	delete(c.Users, key)
	return nil
}

// ParseLimit parses a daily limit given either as a plain number of minutes
// ("90") or as a Go duration ("1h30m", "2h", "45m").
func ParseLimit(s string) (int, error) {
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("invalid time %q: must be greater than zero", s)
		}
		return n, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid time %q: use minutes (90) or a duration (1h30m)", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid time %q: must be greater than zero", s)
	}
	if d%time.Minute != 0 {
		return 0, fmt.Errorf("invalid time %q: must be a whole number of minutes", s)
	}
	return int(d / time.Minute), nil
}

// ClampLimit raises minutes to MinDailyLimitMinutes if needed, reporting
// whether it did.
func ClampLimit(minutes int) (int, bool) {
	if minutes < MinDailyLimitMinutes {
		return MinDailyLimitMinutes, true
	}
	return minutes, false
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
