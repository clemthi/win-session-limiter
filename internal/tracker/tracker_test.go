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
