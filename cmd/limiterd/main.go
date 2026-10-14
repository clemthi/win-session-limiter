// Command limiterd is the Win Session Limiter service binary. It supports
// user-management (add/edit/del) and install/uninstall subcommands, plus the
// run/debug entry points used by the Windows Service Control Manager and by
// interactive testing.
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"

	"winsessionlimiter/internal/config"
	"winsessionlimiter/internal/install"
	"winsessionlimiter/internal/service"
)

func usage(w io.Writer) {
	fmt.Fprintf(w, `Usage: %s <command> [arguments]

User management (requires elevation):
  add <user> <time>   Start limiting <user> to <time> per day
  edit <user> <time>  Change the daily limit of an already limited <user>
  del <user>          Stop limiting <user>

  <time> is a number of minutes (90) or a duration (1h30m, 2h, 45m).
  Limits below %[2]d minutes are raised to %[2]d. Changes apply to the running
  service within one poll interval; no restart is needed.

Service:
  install             Register and start the Windows service (requires elevation)
  uninstall           Stop and remove the Windows service (requires elevation)
  run                 Entry point invoked by the Service Control Manager
  debug               Run the same logic in the foreground console, for testing

  help                Show this help
`, filepath.Base(os.Args[0]), config.MinDailyLimitMinutes)
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stdout)
		return
	}

	args := os.Args[2:]
	var err error
	switch os.Args[1] {
	case "help", "-h", "--help", "/?":
		usage(os.Stdout)
		return
	case "add":
		err = doAdd(args)
	case "edit":
		err = doEdit(args)
	case "del":
		err = doDel(args)
	case "install":
		err = doInstall()
	case "uninstall":
		err = doUninstall()
	case "run":
		err = doRun()
	case "debug":
		err = doDebug()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q.\n\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
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
