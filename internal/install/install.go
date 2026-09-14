// Package install handles one-time setup: creating the ACL-locked data
// directory with a default config, and registering/removing the Windows
// service.
package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"winsessionlimiter/internal/config"
	"winsessionlimiter/internal/service"
)

const (
	ProgramDataDir = `C:\ProgramData\WinSessionLimiter`
	DisplayName    = "Win Session Limiter"
	Description    = "Enforces daily logon time limits for monitored user accounts."
)

var ErrNotElevated = errors.New("this command must be run from an elevated (Administrator) prompt")

func RequireElevated() error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return ErrNotElevated
	}
	return nil
}

// EnsureDataDir creates ProgramDataDir with a default config if missing and
// locks its ACL down to Administrators and SYSTEM only, so the monitored
// standard-user account can't read, edit, or delete the config/state files.
func EnsureDataDir() error {
	if err := os.MkdirAll(ProgramDataDir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", ProgramDataDir, err)
	}

	cfgPath := filepath.Join(ProgramDataDir, config.FileName)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := config.Save(cfgPath, config.Default()); err != nil {
			return fmt.Errorf("writing default config: %w", err)
		}
	}

	return lockDownACL(ProgramDataDir)
}

// lockDownACL replaces inherited permissions with an explicit grant of full
// control to Administrators and SYSTEM only.
func lockDownACL(dir string) error {
	cmd := exec.Command("icacls", dir,
		"/inheritance:r",
		"/grant:r", "*S-1-5-32-544:(OI)(CI)F", // Administrators
		"/grant:r", "*S-1-5-18:(OI)(CI)F", // SYSTEM
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("icacls %s: %w: %s", dir, err, out)
	}
	return nil
}

// Service installs (or updates) and starts the Windows service, pointing it
// at exePath as the binary to run.
func Service(exePath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connecting to service control manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(service.Name)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %q is already installed; run uninstall first to reinstall", service.Name)
	}

	s, err = m.CreateService(service.Name, exePath, mgr.Config{
		StartType:   mgr.StartAutomatic,
		DisplayName: DisplayName,
		Description: Description,
	}, "run")
	if err != nil {
		return fmt.Errorf("creating service: %w", err)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("starting service: %w", err)
	}
	return nil
}

// Uninstall stops and removes the service. Data files under ProgramDataDir
// are left in place.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connecting to service control manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(service.Name)
	if err != nil {
		return fmt.Errorf("service %q is not installed: %w", service.Name, err)
	}
	defer s.Close()

	_, _ = s.Control(svc.Stop)

	if err := s.Delete(); err != nil {
		return fmt.Errorf("deleting service: %w", err)
	}
	return nil
}
