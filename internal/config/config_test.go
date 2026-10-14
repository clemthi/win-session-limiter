package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLimit(t *testing.T) {
	valid := map[string]int{
		"90":    90,
		"45m":   45,
		"2h":    120,
		"1h30m": 90,
	}
	for in, want := range valid {
		got, err := ParseLimit(in)
		if err != nil || got != want {
			t.Errorf("ParseLimit(%q) = (%d, %v), want (%d, nil)", in, got, err, want)
		}
	}

	for _, in := range []string{"0", "-5", "0m", "-1h", "90s", "1h30m15s", "abc", ""} {
		if got, err := ParseLimit(in); err == nil {
			t.Errorf("ParseLimit(%q) = %d, want an error", in, got)
		}
	}
}

func TestClampLimit(t *testing.T) {
	below := MinDailyLimitMinutes - 1
	if got, raised := ClampLimit(below); got != MinDailyLimitMinutes || !raised {
		t.Errorf("ClampLimit(%d) = (%d, %v), want (%d, true)", below, got, raised, MinDailyLimitMinutes)
	}
	if got, raised := ClampLimit(MinDailyLimitMinutes); got != MinDailyLimitMinutes || raised {
		t.Errorf("ClampLimit(%d) = (%d, %v), want unchanged", MinDailyLimitMinutes, got, raised)
	}
	if got, raised := ClampLimit(90); got != 90 || raised {
		t.Errorf("ClampLimit(90) = (%d, %v), want (90, false)", got, raised)
	}
}

func TestAddEditDeleteUser(t *testing.T) {
	c := Default()

	if err := c.AddUser("Kid", 60); err != nil {
		t.Fatal(err)
	}
	if err := c.AddUser("kid", 90); !errors.Is(err, ErrUserExists) {
		t.Fatalf("adding a case-insensitive duplicate: got %v, want ErrUserExists", err)
	}

	if err := c.EditUser("KID", 120); err != nil {
		t.Fatal(err)
	}
	if uc := c.Users["Kid"]; uc.DailyLimitMinutes != 120 {
		t.Fatalf("edit should update the existing key: got users %v", c.Users)
	}
	if len(c.Users) != 1 {
		t.Fatalf("edit should not add a new key: got users %v", c.Users)
	}

	if err := c.EditUser("nobody", 60); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("editing a missing user: got %v, want ErrUserNotFound", err)
	}
	if err := c.DeleteUser("nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("deleting a missing user: got %v, want ErrUserNotFound", err)
	}

	if err := c.DeleteUser("kid"); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Lookup("Kid"); ok {
		t.Fatalf("user should be gone after delete: got users %v", c.Users)
	}
}

func TestEditedConfigRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	c := Default()
	if err := c.AddUser("kid", 90); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if uc, ok := loaded.Lookup("kid"); !ok || uc.DailyLimitMinutes != 90 {
		t.Fatalf("got (%v, %v) after reload, want 90 minutes", uc, ok)
	}
}

func TestLoadEnforcesMinimumsAndMandatoryWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	raw := fmt.Sprintf(`{
		"pollIntervalSeconds": 15,
		"warningsMinutesBefore": [15, 10],
		"users": {
			"kid": { "dailyLimitMinutes": %d }
		}
	}`, MinDailyLimitMinutes-1)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if uc, _ := c.Lookup("kid"); uc.DailyLimitMinutes != MinDailyLimitMinutes {
		t.Fatalf("got DailyLimitMinutes=%d, want floor of %d", uc.DailyLimitMinutes, MinDailyLimitMinutes)
	}
	for _, m := range MandatoryWarningsMinutes {
		if !containsInt(c.WarningsMinutesBefore, m) {
			t.Fatalf("expected mandatory %d-minute warning to be present in %v", m, c.WarningsMinutesBefore)
		}
	}
}

func TestLoadLeavesValidLimitsAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	raw := `{"users": {"kid": {"dailyLimitMinutes": 120}}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if uc, _ := c.Lookup("kid"); uc.DailyLimitMinutes != 120 {
		t.Fatalf("got %d, want 120 (should not be clamped)", uc.DailyLimitMinutes)
	}
}
