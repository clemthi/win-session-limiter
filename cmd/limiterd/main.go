// Command limiterd is the Win Session Limiter service binary. It supports
// install/uninstall subcommands plus the run/debug entry points used by the
// Windows Service Control Manager and by interactive testing.
package main

import (
	"fmt"
	"log"
	"os"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"

	"winsessionlimiter/internal/install"
	"winsessionlimiter/internal/service"
)

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: %s <command>

Commands:
  install    Register and start the Windows service (requires elevation)
  uninstall  Stop and remove the Windows service (requires elevation)
  run        Entry point invoked by the Service Control Manager
  debug      Run the same logic in the foreground console, for testing
`, os.Args[0])
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "install":
		err = doInstall()
	case "uninstall":
		err = doUninstall()
	case "run":
		err = doRun()
	case "debug":
		err = doDebug()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func doInstall() error {
	if err := install.RequireElevated(); err != nil {
		return err
	}
	if err := install.EnsureDataDir(); err != nil {
		return err
	}

	// Best-effort: registering an event source twice returns an error we
	// can ignore (it just means a previous install already did this).
	_ = eventlog.InstallAsEventCreate(service.Name, eventlog.Error|eventlog.Warning|eventlog.Info)

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}
	if err := install.Service(exePath); err != nil {
		return err
	}
	fmt.Printf("%s installed and started.\n", service.Name)
	fmt.Printf("Config/state: %s\n", install.ProgramDataDir)
	return nil
}

func doUninstall() error {
	if err := install.RequireElevated(); err != nil {
		return err
	}
	if err := install.Uninstall(); err != nil {
		return err
	}
	_ = eventlog.Remove(service.Name)
	fmt.Printf("%s stopped and removed. Data under %s was left in place.\n", service.Name, install.ProgramDataDir)
	return nil
}

func doRun() error {
	elog, err := eventlog.Open(service.Name)
	if err != nil {
		return fmt.Errorf("opening event log: %w", err)
	}
	defer elog.Close()

	handler, err := service.NewHandler(install.ProgramDataDir, elog)
	if err != nil {
		elog.Error(1, fmt.Sprintf("initializing: %v", err))
		return err
	}
	return svc.Run(service.Name, handler)
}

// consoleLogger implements service.Logger by printing to stdout, used by
// `debug` so the service logic can be exercised without installing anything.
type consoleLogger struct{}

func (consoleLogger) Info(eventID uint32, msg string) error {
	log.Printf("[info] %s", msg)
	return nil
}

func (consoleLogger) Warning(eventID uint32, msg string) error {
	log.Printf("[warn] %s", msg)
	return nil
}

func (consoleLogger) Error(eventID uint32, msg string) error {
	log.Printf("[error] %s", msg)
	return nil
}

func doDebug() error {
	if err := os.MkdirAll(install.ProgramDataDir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", install.ProgramDataDir, err)
	}
	handler, err := service.NewHandler(install.ProgramDataDir, consoleLogger{})
	if err != nil {
		return err
	}
	fmt.Println("Running in foreground. Press Ctrl+C to stop.")
	return debug.Run(service.Name, handler)
}
