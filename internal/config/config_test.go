package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnforcesMinimumsAndMandatoryWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	raw := `{
		"pollIntervalSeconds": 15,
		"warningsMinutesBefore": [15, 10],
		"users": {
			"kid": { "dailyLimitMinutes": 2 }
		}
	}`
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
	if !containsInt(c.WarningsMinutesBefore, MandatoryWarningMinutes) {
		t.Fatalf("expected mandatory %d-minute warning to be present in %v", MandatoryWarningMinutes, c.WarningsMinutesBefore)
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
