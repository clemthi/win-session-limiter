// Package wts wraps the subset of wtsapi32.dll needed to enumerate
// sessions, resolve a session's username, pop up a message in a session,
// and force a session logoff. golang.org/x/sys/windows does not bind these
// Terminal Services APIs, so we call them directly via syscall.
package wts

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modwtsapi32 = windows.NewLazySystemDLL("wtsapi32.dll")

	procWTSEnumerateSessionsW       = modwtsapi32.NewProc("WTSEnumerateSessionsW")
	procWTSQuerySessionInformationW = modwtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory               = modwtsapi32.NewProc("WTSFreeMemory")
	procWTSSendMessageW             = modwtsapi32.NewProc("WTSSendMessageW")
	procWTSLogoffSession            = modwtsapi32.NewProc("WTSLogoffSession")
)

const wtsCurrentServerHandle = 0 // WTS_CURRENT_SERVER_HANDLE

// WTS_CONNECTSTATE_CLASS values we care about.
const (
	wtsActive       = 0
	wtsConnected    = 1
	wtsConnectQuery = 2
	wtsShadow       = 3
)

// WTS_INFO_CLASS
const wtsUserName = 5
const wtsDomainName = 7

type wtsSessionInfo struct {
	SessionID      uint32
	WinStationName *uint16
	State          uint32
}

// Session describes one active Terminal Services session on this machine.
type Session struct {
	ID       uint32
	Username string
	Domain   string
	Active   bool // true for interactive/console-connected states we count
}

// EnumerateSessions lists all sessions currently known to the terminal
// services subsystem, resolving each one's username/domain.
func EnumerateSessions() ([]Session, error) {
	var (
		pSessionInfo *wtsSessionInfo
		count        uint32
	)
	r1, _, err := procWTSEnumerateSessionsW.Call(
		uintptr(wtsCurrentServerHandle),
		0,
		1,
		uintptr(unsafe.Pointer(&pSessionInfo)),
		uintptr(unsafe.Pointer(&count)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("WTSEnumerateSessionsW: %w", err)
	}
	defer procWTSFreeMemory.Call(uintptr(unsafe.Pointer(pSessionInfo)))

	entries := unsafe.Slice(pSessionInfo, count)
	sessions := make([]Session, 0, count)
	for _, e := range entries {
		user, _ := queryString(e.SessionID, wtsUserName)
		if user == "" {
			continue // system sessions (e.g. Services, session 0) have no user
		}
		domain, _ := queryString(e.SessionID, wtsDomainName)
		sessions = append(sessions, Session{
			ID:       e.SessionID,
			Username: user,
			Domain:   domain,
			Active:   e.State == wtsActive || e.State == wtsConnected,
		})
	}
	return sessions, nil
}

// QueryUsername resolves the username for a single session ID.
func QueryUsername(sessionID uint32) (string, error) {
	return queryString(sessionID, wtsUserName)
}

func queryString(sessionID uint32, infoClass uint32) (string, error) {
	var (
		pBuf  *uint16
		bytes uint32
	)
	r1, _, err := procWTSQuerySessionInformationW.Call(
		uintptr(wtsCurrentServerHandle),
		uintptr(sessionID),
		uintptr(infoClass),
		uintptr(unsafe.Pointer(&pBuf)),
		uintptr(unsafe.Pointer(&bytes)),
	)
	if r1 == 0 {
		return "", fmt.Errorf("WTSQuerySessionInformationW: %w", err)
	}
	defer procWTSFreeMemory.Call(uintptr(unsafe.Pointer(pBuf)))
	if pBuf == nil {
		return "", nil
	}
	return windows.UTF16PtrToString(pBuf), nil
}

// SendMessage pops up a modal message box in the given session. It does not
// block waiting for the user to respond (wait=false), so the caller isn't
// stalled by an unattended dialog.
func SendMessage(sessionID uint32, title, text string, timeoutSeconds uint32) error {
	titleUTF16, err := windows.UTF16FromString(title)
	if err != nil {
		return err
	}
	textUTF16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	var response uint32
	const mbOK = 0x00000000
	const mbIconWarning = 0x00000030
	const waitFalse = 0

	r1, _, err2 := procWTSSendMessageW.Call(
		uintptr(wtsCurrentServerHandle),
		uintptr(sessionID),
		uintptr(unsafe.Pointer(&titleUTF16[0])),
		uintptr(len(titleUTF16)*2), // byte length, excluding nothing special
		uintptr(unsafe.Pointer(&textUTF16[0])),
		uintptr(len(textUTF16)*2),
		uintptr(mbOK|mbIconWarning),
		uintptr(timeoutSeconds),
		uintptr(unsafe.Pointer(&response)),
		uintptr(waitFalse),
	)
	if r1 == 0 {
		return fmt.Errorf("WTSSendMessageW: %w", err2)
	}
	return nil
}

// LogoffSession ends the given session. wait=false requests a graceful
// logoff (apps get a chance to react to WM_QUERYENDSESSION) rather than an
// immediate forced termination.
func LogoffSession(sessionID uint32) error {
	const waitFalse = 0
	r1, _, err := procWTSLogoffSession.Call(
		uintptr(wtsCurrentServerHandle),
		uintptr(sessionID),
		uintptr(waitFalse),
	)
	if r1 == 0 {
		return fmt.Errorf("WTSLogoffSession: %w", err)
	}
	return nil
}

// Errno-compatibility helper retained for clarity at call sites that check
// for "no such session" after a session has already logged off.
func IsNoSuchSession(err error) bool {
	errno, ok := err.(syscall.Errno)
	return ok && errno == windows.ERROR_NOT_FOUND
}
