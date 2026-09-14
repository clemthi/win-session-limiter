package tracker

import "testing"

func TestRemainingSeconds(t *testing.T) {
	if got := RemainingSeconds(0, 60); got != 3600 {
		t.Fatalf("got %d, want 3600", got)
	}
	if got := RemainingSeconds(3700, 60); got != -100 {
		t.Fatalf("got %d, want -100", got)
	}
}

func TestShouldLogoff(t *testing.T) {
	if ShouldLogoff(1) {
		t.Fatal("1 second remaining should not trigger logoff")
	}
	if !ShouldLogoff(0) {
		t.Fatal("0 seconds remaining should trigger logoff")
	}
	if !ShouldLogoff(-5) {
		t.Fatal("negative remaining should trigger logoff")
	}
}

func TestNextWarningFiresHighestUnfiredThresholdOnly(t *testing.T) {
	fired := map[int]bool{}
	thresholds := []int{15, 10, 5, 1}

	// 20 minutes remaining: no threshold crossed yet.
	if _, ok := NextWarning(20*60, thresholds, fired); ok {
		t.Fatal("expected no warning at 20 minutes remaining")
	}

	// 12 minutes remaining: crosses only the 15 minute threshold.
	got, ok := NextWarning(12*60, thresholds, fired)
	if !ok || got != 15 {
		t.Fatalf("got (%d, %v), want (15, true)", got, ok)
	}
	if !fired[15] {
		t.Fatal("expected 15 to be marked fired")
	}

	// Same remaining time again: 15 already fired and 10 hasn't been
	// crossed yet, so nothing more should fire.
	if _, ok := NextWarning(12*60, thresholds, fired); ok {
		t.Fatal("expected no more warnings at 12 minutes remaining")
	}

	// Dropping to 8 minutes remaining crosses the 10 minute threshold.
	got, ok = NextWarning(8*60, thresholds, fired)
	if !ok || got != 10 {
		t.Fatalf("got (%d, %v), want (10, true)", got, ok)
	}

	// Dropping to 4 minutes remaining crosses the 5 minute threshold.
	got, ok = NextWarning(4*60, thresholds, fired)
	if !ok || got != 5 {
		t.Fatalf("got (%d, %v), want (5, true)", got, ok)
	}
}

func TestNextWarningNoThresholds(t *testing.T) {
	if _, ok := NextWarning(0, nil, map[int]bool{}); ok {
		t.Fatal("expected no warning with empty threshold list")
	}
}

func TestSeedFiredSuppressesAlreadyCrossedThresholds(t *testing.T) {
	// A user logs back in with only 6 minutes left today (e.g. they used
	// 24 of a 30 minute budget in an earlier session). The 15 and 10
	// minute thresholds are already behind them and must not fire later
	// with a stale "15/10 minutes remaining" message.
	thresholds := []int{15, 10, 5, 1}
	fired := map[int]bool{}
	SeedFired(6*60, thresholds, fired)

	if !fired[15] || !fired[10] {
		t.Fatalf("expected 15 and 10 to be pre-suppressed, got %v", fired)
	}
	if fired[5] || fired[1] {
		t.Fatalf("expected 5 and 1 to remain eligible, got %v", fired)
	}

	// 15 and 10 must never fire from here on...
	if _, ok := NextWarning(6*60, thresholds, fired); ok {
		t.Fatal("expected no warning immediately after seeding at 6 minutes remaining")
	}
	// ...but 5 still fires accurately once genuinely reached.
	got, ok := NextWarning(5*60, thresholds, fired)
	if !ok || got != 5 {
		t.Fatalf("got (%d, %v), want (5, true)", got, ok)
	}
}
