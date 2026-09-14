// Package tracker holds the pure decision logic for usage accounting:
// remaining-time math and warning-threshold firing. It has no Windows
// dependency so it can be unit tested directly.
package tracker

import "sort"

// RemainingSeconds returns how many seconds are left before the daily limit
// is hit. It can be negative once the limit has been exceeded.
func RemainingSeconds(usedSeconds int64, limitMinutes int) int64 {
	return int64(limitMinutes)*60 - usedSeconds
}

// ShouldLogoff reports whether the daily limit has been reached.
func ShouldLogoff(remainingSeconds int64) bool {
	return remainingSeconds <= 0
}

// NextWarning returns the largest warning threshold (in minutes) that
// remainingSeconds has now dropped to or below and that hasn't already
// fired today, so callers fire at most one warning per call. fired is
// mutated to record the threshold as sent. Returns ok=false if no
// not-yet-fired threshold applies.
func NextWarning(remainingSeconds int64, thresholdsMinutes []int, fired map[int]bool) (int, bool) {
	sorted := append([]int(nil), thresholdsMinutes...)
	sort.Sort(sort.Reverse(sort.IntSlice(sorted)))
	for _, t := range sorted {
		if fired[t] {
			continue
		}
		if remainingSeconds <= int64(t)*60 {
			fired[t] = true
			return t, true
		}
	}
	return 0, false
}

// SeedFired marks, without signaling a warning, every threshold that has
// already been crossed by the time tracking starts (e.g. a user logs back
// in after already using up most of today's budget in an earlier session).
// Without this, NextWarning would fire each of those thresholds in turn
// over the next few calls with a stale, inaccurate "N minutes remaining"
// message even though far less than N minutes are actually left.
func SeedFired(remainingSeconds int64, thresholdsMinutes []int, fired map[int]bool) {
	for _, t := range thresholdsMinutes {
		if remainingSeconds <= int64(t)*60 {
			fired[t] = true
		}
	}
}
