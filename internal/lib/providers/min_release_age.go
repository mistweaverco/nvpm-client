package providers

import (
	"errors"
	"fmt"
	"time"
)

// MinReleaseAgeTooSoonError is returned when a version was discovered too recently
// to install/update under the configured min-release-age policy.
// This is an intentional safety skip, not a hard failure.
type MinReleaseAgeTooSoonError struct {
	SourceID  string
	Version   string
	Age       time.Duration
	Remaining time.Duration
}

func (e *MinReleaseAgeTooSoonError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf(
		"%s@%s is waiting for min-release-age - available in %s (use --force to override)",
		e.SourceID, e.Version, formatFriendlyDuration(e.Remaining),
	)
}

// AsMinReleaseAgeTooSoon reports whether err is (or wraps) a min-release-age wait.
func AsMinReleaseAgeTooSoon(err error) (*MinReleaseAgeTooSoonError, bool) {
	var tooSoon *MinReleaseAgeTooSoonError
	if errors.As(err, &tooSoon) {
		return tooSoon, true
	}
	return nil, false
}

// formatFriendlyDuration formats a duration for human-facing CLI messages
// (e.g. "5 days", "3 hours", "1 minute").
func formatFriendlyDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d >= 14*24*time.Hour {
		weeks := int((d + 7*24*time.Hour - 1) / (7 * 24 * time.Hour))
		if weeks == 1 {
			return "1 week"
		}
		return fmt.Sprintf("%d weeks", weeks)
	}
	if d >= 24*time.Hour {
		days := int((d + 24*time.Hour - 1) / (24 * time.Hour))
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	if d >= time.Hour {
		hours := int((d + time.Hour - 1) / time.Hour)
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	minutes := int((d + time.Minute - 1) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	if minutes == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", minutes)
}
