// Package state persists today's per-user accumulated logon seconds.
package state

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

const FileName = "state.json"

// dateLayout is a plain YYYY-MM-DD local-date string, deliberately not a
// full timestamp: rollover is a date comparison, not a duration comparison.
const dateLayout = "2006-01-02"

type State struct {
	Date  string           `json:"date"`
	Usage map[string]int64 `json:"usage"` // username -> accumulated seconds today
}

func New(today string) *State {
	return &State{Date: today, Usage: map[string]int64{}}
}

func Load(path, today string) (*State, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return New(today), nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Usage == nil {
		s.Usage = map[string]int64{}
	}
	return &s, nil
}

// Save writes s to path atomically (temp file + rename).
func Save(path string, s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Store wraps a State with the mutation and persistence logic used by the
// tick loop: date rollover, per-user accrual, and lookups, all safe for
// concurrent use.
type Store struct {
	path string

	mu sync.Mutex
	s  *State
}

func NewStore(path, today string) (*Store, error) {
	s, err := Load(path, today)
	if err != nil {
		return nil, err
	}
	st := &Store{path: path, s: s}
	if st.s.Date != today {
		st.s = New(today)
		if err := Save(st.path, st.s); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// RolloverIfNeeded resets usage when today has changed since the last save.
// Returns true if a rollover happened.
func (st *Store) RolloverIfNeeded(today string) (bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.s.Date == today {
		return false, nil
	}
	st.s = New(today)
	return true, Save(st.path, st.s)
}

// AddSeconds adds delta seconds of usage for username and persists the
// updated state, returning the new total for that user.
func (st *Store) AddSeconds(username string, delta int64) (int64, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	key := canonicalKey(st.s.Usage, username)
	st.s.Usage[key] += delta
	total := st.s.Usage[key]
	return total, Save(st.path, st.s)
}

// UsedSeconds returns the current accumulated seconds for username.
func (st *Store) UsedSeconds(username string) int64 {
	st.mu.Lock()
	defer st.mu.Unlock()
	key := canonicalKey(st.s.Usage, username)
	return st.s.Usage[key]
}

// canonicalKey finds an existing case-insensitive match for username in m,
// so accrual keys off of whatever case the user was first seen with.
func canonicalKey(m map[string]int64, username string) string {
	for k := range m {
		if strings.EqualFold(k, username) {
			return k
		}
	}
	return username
}
