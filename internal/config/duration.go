package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ParseDuration parses Go duration strings and also accepts a day unit ("d"),
// e.g. "7d", "60d", "1h30m", "0".
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if s == "0" {
		return 0, nil
	}

	// Fast path: standard Go durations (no day unit).
	if !strings.ContainsAny(s, "dD") || !hasDayUnit(s) {
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, err
		}
		if d < 0 {
			return 0, fmt.Errorf("negative duration")
		}
		return d, nil
	}

	var total time.Duration
	i := 0
	for i < len(s) {
		// Skip whitespace
		for i < len(s) && unicode.IsSpace(rune(s[i])) {
			i++
		}
		if i >= len(s) {
			break
		}

		start := i
		if s[i] == '+' || s[i] == '-' {
			i++
		}
		for i < len(s) && (unicode.IsDigit(rune(s[i])) || s[i] == '.') {
			i++
		}
		if i == start || (i == start+1 && (s[start] == '+' || s[start] == '-')) {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		numStr := s[start:i]
		if i >= len(s) {
			return 0, fmt.Errorf("missing unit in duration %q", s)
		}

		unitStart := i
		for i < len(s) && unicode.IsLetter(rune(s[i])) {
			i++
		}
		unit := s[unitStart:i]
		if unit == "" {
			return 0, fmt.Errorf("missing unit in duration %q", s)
		}

		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration number %q: %w", numStr, err)
		}
		if num < 0 {
			return 0, fmt.Errorf("negative duration")
		}

		var unitDur time.Duration
		switch unit {
		case "ns", "us", "µs", "ms", "s", "m", "h":
			d, err := time.ParseDuration(numStr + unit)
			if err != nil {
				return 0, err
			}
			unitDur = d
		case "d":
			unitDur = time.Duration(num * float64(24*time.Hour))
		default:
			return 0, fmt.Errorf("unknown unit %q in duration %q", unit, s)
		}
		total += unitDur
	}
	return total, nil
}

func hasDayUnit(s string) bool {
	for i := 0; i < len(s); i++ {
		if (s[i] == 'd' || s[i] == 'D') && (i == 0 || !unicode.IsLetter(rune(s[i-1]))) {
			// "d" as a unit: preceded by digit/dot/sign, not part of "ms" etc.
			if i > 0 && (unicode.IsDigit(rune(s[i-1])) || s[i-1] == '.') {
				return true
			}
		}
	}
	return false
}
