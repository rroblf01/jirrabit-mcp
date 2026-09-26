package jira

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseJiraDuration converts a Jira time-tracking string into whole minutes.
//
// Jira sends durations as "2h 30m" and accepts "1d", "4w", "90m" or a bare
// number of minutes. jirrabit stores whole minutes, so everything is normalised
// here rather than in each tool.
//
// Supported units, matching Jira's own: w (weeks), d (days), h (hours), m
// (minutes). Seconds are not part of Jira's format; Jira's `timeSpentSeconds`
// is a separate field, handled by the caller.
func ParseJiraDuration(value string) (int, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return 0, fmt.Errorf("empty duration")
	}
	// Tolerate a leading "+" and any internal spaces, e.g. "2h 30m".
	raw = strings.ReplaceAll(raw, " ", "")

	// A bare number means minutes in Jira.
	if minutes, err := strconv.Atoi(raw); err == nil {
		if minutes <= 0 {
			return 0, fmt.Errorf("duration must be greater than 0, got %q", value)
		}
		return minutes, nil
	}

	perUnit := map[byte]int{'w': 7 * 24 * 60, 'd': 24 * 60, 'h': 60, 'm': 1}

	var total, digits int
	seenDigit, seenUnit := false, false
	for i := 0; i < len(raw); i++ {
		char := raw[i]
		switch {
		case char >= '0' && char <= '9':
			if seenUnit {
				return 0, fmt.Errorf("invalid duration %q: number after a unit", value)
			}
			digits = digits*10 + int(char-'0')
			seenDigit = true
		case char == '+' && i == 0:
			// Explicitly positive, e.g. "+2h". Fine, ignore.
		default:
			perMinute, ok := perUnit[char]
			if !ok {
				return 0, fmt.Errorf("invalid duration %q: unknown unit %q (use w, d, h or m)", value, string(char))
			}
			if !seenDigit {
				return 0, fmt.Errorf("invalid duration %q: unit %q without a number", value, string(char))
			}
			total += digits * perMinute
			digits, seenDigit, seenUnit = 0, false, true
		}
	}
	if seenDigit {
		return 0, fmt.Errorf("invalid duration %q: trailing number with no unit", value)
	}
	if total <= 0 {
		return 0, fmt.Errorf("duration must be greater than 0, got %q", value)
	}
	return total, nil
}
