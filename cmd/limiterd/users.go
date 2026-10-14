package main

import (
	"fmt"
	"path/filepath"

	"winsessionlimiter/internal/config"
	"winsessionlimiter/internal/install"
)

func doAdd(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: limiterd add <user> <time>")
	}
	minutes, note, err := parseAndClamp(args[1])
	if err != nil {
		return err
	}
	return updateConfig(func(c *config.Config) (string, error) {
		if err := c.AddUser(args[0], minutes); err != nil {
			return "", err
		}
		return note + fmt.Sprintf("Added %q with a daily limit of %d minutes.", args[0], minutes), nil
	})
}

func doEdit(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: limiterd edit <user> <time>")
	}
	minutes, note, err := parseAndClamp(args[1])
	if err != nil {
		return err
	}
	return updateConfig(func(c *config.Config) (string, error) {
		if err := c.EditUser(args[0], minutes); err != nil {
			return "", err
		}
		return note + fmt.Sprintf("Set the daily limit of %q to %d minutes.", args[0], minutes), nil
	})
}

func doDel(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: limiterd del <user>")
	}
	return updateConfig(func(c *config.Config) (string, error) {
		if err := c.DeleteUser(args[0]); err != nil {
			return "", err
		}
		return fmt.Sprintf("Removed %q; that account is no longer limited.", args[0]), nil
	})
}

// parseAndClamp parses a <time> argument and applies the minimum, returning
// a warning line (empty if no clamping happened) to print once saved.
func parseAndClamp(s string) (int, string, error) {
	minutes, err := config.ParseLimit(s)
	if err != nil {
		return 0, "", err
	}
	clamped, raised := config.ClampLimit(minutes)
	if !raised {
		return clamped, "", nil
	}
	note := fmt.Sprintf("Warning: %d minutes is below the %d-minute minimum; using %d minutes.\n",
		minutes, config.MinDailyLimitMinutes, clamped)
	return clamped, note, nil
}

// updateConfig loads config.json, applies mutate, saves it back, and prints
// mutate's success message. The running service picks up the change on its
// next poll.
func updateConfig(mutate func(*config.Config) (string, error)) error {
	if err := install.RequireElevated(); err != nil {
		return err
	}
	if err := install.EnsureDataDir(); err != nil {
		return err
	}
	path := filepath.Join(install.ProgramDataDir, config.FileName)
	c, err := config.Load(path)
	if err != nil {
		return err
	}
	msg, err := mutate(c)
	if err != nil {
		return err
	}
	if err := config.Save(path, c); err != nil {
		return err
	}
	fmt.Println(msg)
	return nil
}
