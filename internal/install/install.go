// Package install handles one-time setup: creating the ACL-locked data
// directory with a default config, copying the binary to Program Files,
// and registering/removing the Windows service.
package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"winsessionlimiter/internal/config"
	"winsessionlimiter/internal/service"
)

// The folders and the installed binary are named after the service, so
// renaming the service in service.Name renames them too. DisplayName and
// Description are what services.msc shows; they're kept deliberately
// neutral so the monitored account can't easily spot what the service does.
const (
	ProgramDataDir = `C:\ProgramData\` + service.Name
	// InstallDir holds the copy of the binary the service runs. Standard
	// users can't write under Program Files, so the monitored account can't
	// replace or delete it.
	InstallDir  = `C:\Program Files\` + service.Name
	ExeName     = service.Name + ".exe"
	DisplayName = "Session Helper"
	Description = "Provides support for user session management."
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

// Service copies srcExe into InstallDir, then registers and starts the
// Windows service on that copy. It returns the installed binary's path.
func Service(srcExe string) (string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return "", fmt.Errorf("connecting to service control manager: %w", err)
	}
	defer m.Disconnect()

	// Checked before copying: the binary of an installed service is
	// usually running and locked, so overwriting it would fail anyway.
	s, err := m.OpenService(service.Name)
	if err == nil {
		s.Close()
		return "", fmt.Errorf("service %q is already installed; run uninstall first to reinstall", service.Name)
	}

	exePath := filepath.Join(InstallDir, ExeName)
	if err := copyBinary(srcExe, exePath); err != nil {
		return "", err
	}

	s, err = m.CreateService(service.Name, exePath, mgr.Config{
		StartType:   mgr.StartAutomatic,
		DisplayName: DisplayName,
		Description: Description,
	}, "run")
	if err != nil {
		return "", fmt.Errorf("creating service: %w", err)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return "", fmt.Errorf("starting service: %w", err)
	}
	return exePath, nil
}

// copyBinary copies src to dst, creating dst's folder if needed. Copying a
// file onto itself (installing from the installed copy) is a no-op rather
// than a truncation.
func copyBinary(src, dst string) error {
	if srcInfo, err := os.Stat(src); err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	} else if dstInfo, err := os.Stat(dst); err == nil && os.SameFile(srcInfo, dstInfo) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("copying to %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("copying to %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("copying to %s: %w", dst, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("copying to %s: %w", dst, err)
	}
	return nil
}

// ErrInstallDirInUse means the service was removed but InstallDir couldn't
// be deleted because uninstall is running from the copy inside it.
var ErrInstallDirInUse = errors.New("uninstall is running from the installed copy, so its folder can't be deleted")

// Uninstall stops and removes the service, then deletes InstallDir. Data
// files under ProgramDataDir are left in place.
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

	// The binary stays locked until the service process exits, so wait for
	// the stop to complete before deleting it.
	if status, err := s.Control(svc.Stop); err == nil && status.State != svc.Stopped {
		if err := waitForStop(s, 15*time.Second); err != nil {
			return err
		}
	}

	if err := s.Delete(); err != nil {
		return fmt.Errorf("deleting service: %w", err)
	}

	return removeInstallDir()
}

func waitForStop(s *mgr.Service, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := s.Query()
		if err != nil {
			return fmt.Errorf("querying service status: %w", err)
		}
		if status.State == svc.Stopped {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("service %q did not stop within %s", service.Name, timeout)
}

func removeInstallDir() error {
	if self, err := os.Executable(); err == nil {
		if rel, err := filepath.Rel(InstallDir, self); err == nil && filepath.Dir(rel) == "." {
			return ErrInstallDirInUse
		}
	}
	if err := os.RemoveAll(InstallDir); err != nil {
		return fmt.Errorf("removing %s: %w", InstallDir, err)
	}
	return nil
}
