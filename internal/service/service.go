// Package service implements the Windows service Handler: it wires together
// config, state, tracker and wts to watch session logons/logoffs and
// enforce each monitored user's daily time budget.
package service

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"

	"winsessionlimiter/internal/config"
	"winsessionlimiter/internal/state"
	"winsessionlimiter/internal/tracker"
	"winsessionlimiter/internal/wts"
)

const Name = "WinSessionLimiter"

// Logger is the subset of svc/eventlog.Log used here, so callers can also
// pass a plain stdout logger when running interactively via `debug`.
type Logger interface {
	Info(eventID uint32, msg string) error
	Warning(eventID uint32, msg string) error
	Error(eventID uint32, msg string) error
}

const (
	eventInfo = 1
	eventWarn = 2
	eventErr  = 3
)

// trackedSession is one logged-on session belonging to a monitored user.
type trackedSession struct {
	username    string
	lastAccrual time.Time
	fired       map[int]bool // warning thresholds (minutes) already sent today
}

type wtsSessionNotification struct {
	Size      uint32
	SessionID uint32
}

type Handler struct {
	dataDir string
	log     Logger

	cfg          *config.Watcher
	usage        *state.Store
	sessions     map[uint32]*trackedSession
	pollInterval time.Duration
}

func NewHandler(dataDir string, log Logger) (*Handler, error) {
	cfgPath := filepath.Join(dataDir, config.FileName)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := config.Save(cfgPath, config.Default()); err != nil {
			return nil, fmt.Errorf("writing default config: %w", err)
		}
	}
	cfgWatcher, err := config.NewWatcher(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	statePath := filepath.Join(dataDir, state.FileName)
	usage, err := state.NewStore(statePath, today())
	if err != nil {
		return nil, fmt.Errorf("loading state: %w", err)
	}
	return &Handler{
		dataDir:  dataDir,
		log:      log,
		cfg:      cfgWatcher,
		usage:    usage,
		sessions: map[uint32]*trackedSession{},
	}, nil
}

func today() string {
	return time.Now().Format("2006-01-02")
}

// seedActiveSessions discovers already-logged-on monitored users, so a
// service restart mid-session doesn't lose track of them.
func (h *Handler) seedActiveSessions() {
	sessions, err := wts.EnumerateSessions()
	if err != nil {
		h.log.Warning(eventWarn, fmt.Sprintf("enumerating sessions at startup: %v", err))
		return
	}
	for _, s := range sessions {
		if !s.Active {
			continue
		}
		if _, ok := h.cfg.Current().Lookup(s.Username); !ok {
			continue
		}
		h.trackSession(s.ID, s.Username)
	}
}

func (h *Handler) trackSession(sessionID uint32, username string) {
	if _, exists := h.sessions[sessionID]; exists {
		return
	}
	cfg := h.cfg.Current()
	fired := map[int]bool{}
	if uc, ok := cfg.Lookup(username); ok {
		// If the user already has little time left today (e.g. they're
		// logging back in after an earlier session), suppress any
		// warning threshold already behind them instead of firing it on
		// an upcoming tick with a stale "N minutes remaining" message.
		remaining := tracker.RemainingSeconds(h.usage.UsedSeconds(username), uc.DailyLimitMinutes)
		tracker.SeedFired(remaining, cfg.WarningsMinutesBefore, fired)
	}
	h.sessions[sessionID] = &trackedSession{
		username:    username,
		lastAccrual: time.Now(),
		fired:       fired,
	}
	h.log.Info(eventInfo, fmt.Sprintf("tracking session %d for monitored user %q", sessionID, username))
}

func (h *Handler) untrackSession(sessionID uint32) {
	delete(h.sessions, sessionID)
}

func (h *Handler) handleSessionChange(eventType uint32, eventData uintptr) {
	if eventData == 0 {
		return
	}
	// eventData is a pointer, valid only for this call, to a
	// WTSSESSION_NOTIFICATION struct that the SCM fills in synchronously
	// for SERVICE_CONTROL_SESSIONCHANGE; go vet's unsafeptr check can't
	// see that provenance across the svc package's uintptr-typed field,
	// hence the "possible misuse" warning it flags here.
	notif := (*wtsSessionNotification)(unsafe.Pointer(eventData)) //nolint:govet
	sessionID := notif.SessionID

	switch eventType {
	case windows.WTS_SESSION_LOGON:
		username, err := wts.QueryUsername(sessionID)
		if err != nil || username == "" {
			return
		}
		if _, ok := h.cfg.Current().Lookup(username); !ok {
			return
		}
		h.trackSession(sessionID, username)
	case windows.WTS_SESSION_LOGOFF:
		h.untrackSession(sessionID)
	}
}

// tick accrues elapsed time for every tracked session, persists it, and
// fires warnings / forces logoff as each user's budget dictates.
func (h *Handler) tick(ticker *time.Ticker) {
	if err := h.cfg.Refresh(); err != nil {
		h.log.Warning(eventWarn, fmt.Sprintf("reloading config: %v", err))
	}
	cfg := h.cfg.Current()

	if newInterval := cfg.PollInterval(); newInterval != h.pollInterval {
		h.pollInterval = newInterval
		ticker.Reset(newInterval)
	}

	if rolled, err := h.usage.RolloverIfNeeded(today()); err != nil {
		h.log.Warning(eventWarn, fmt.Sprintf("rolling over state: %v", err))
	} else if rolled {
		for _, s := range h.sessions {
			s.fired = map[int]bool{}
		}
		h.log.Info(eventInfo, "daily usage rolled over")
	}

	now := time.Now()
	for sessionID, sess := range h.sessions {
		uc, ok := cfg.Lookup(sess.username)
		if !ok {
			// No longer monitored (config edited); stop accruing/enforcing.
			continue
		}

		elapsed := now.Sub(sess.lastAccrual)
		sess.lastAccrual = now
		total, err := h.usage.AddSeconds(sess.username, int64(elapsed.Seconds()))
		if err != nil {
			h.log.Warning(eventWarn, fmt.Sprintf("saving usage for %q: %v", sess.username, err))
			continue
		}

		remaining := tracker.RemainingSeconds(total, uc.DailyLimitMinutes)

		if tracker.ShouldLogoff(remaining) {
			h.enforceLogoff(sessionID, sess.username)
			continue
		}

		if minutes, ok := tracker.NextWarning(remaining, cfg.WarningsMinutesBefore, sess.fired); ok {
			h.warn(sessionID, sess.username, minutes)
		}
	}
}

func (h *Handler) warn(sessionID uint32, username string, minutesRemaining int) {
	msg := fmt.Sprintf("%d minute(s) of your daily computer time remain. You will be logged off automatically when your time runs out.", minutesRemaining)
	if err := wts.SendMessage(sessionID, "Time limit warning", msg, 15); err != nil {
		h.log.Warning(eventWarn, fmt.Sprintf("sending warning to %q (session %d): %v", username, sessionID, err))
		return
	}
	h.log.Info(eventInfo, fmt.Sprintf("warned %q (session %d): %d minute(s) remaining", username, sessionID, minutesRemaining))
}

func (h *Handler) enforceLogoff(sessionID uint32, username string) {
	_ = wts.SendMessage(sessionID, "Time limit reached", "Your daily computer time is up. You are being logged off now.", 10)
	if err := wts.LogoffSession(sessionID); err != nil {
		h.log.Error(eventErr, fmt.Sprintf("forcing logoff of %q (session %d): %v", username, sessionID, err))
		return
	}
	h.log.Info(eventInfo, fmt.Sprintf("logged off %q (session %d): daily limit reached", username, sessionID))
	h.untrackSession(sessionID)
}

// Execute implements svc.Handler.
func (h *Handler) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptSessionChange

	changes <- svc.Status{State: svc.StartPending}

	h.seedActiveSessions()

	h.pollInterval = h.cfg.Current().PollInterval()
	ticker := time.NewTicker(h.pollInterval)
	defer ticker.Stop()

	changes <- svc.Status{State: svc.Running, Accepts: accepted}
	h.log.Info(eventInfo, "service started")

loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				break loop
			case svc.SessionChange:
				h.handleSessionChange(c.EventType, c.EventData)
			}
		case <-ticker.C:
			h.tick(ticker)
		}
	}

	changes <- svc.Status{State: svc.StopPending}
	h.log.Info(eventInfo, "service stopping")
	return false, 0
}
