package nvpm

import (
	"fmt"
	"time"
)

// formatAgeAgo formats a duration as "N days ago", or "1 year and N days ago" when > 365 days.
func formatAgeAgo(age time.Duration) string {
	if age < 0 {
		age = 0
	}
	days := int(age / (24 * time.Hour))
	if days > 365 {
		years := days / 365
		remDays := days % 365
		if remDays == 0 {
			if years == 1 {
				return "1 year ago"
			}
			return fmt.Sprintf("%d years ago", years)
		}
		if years == 1 {
			if remDays == 1 {
				return "1 year and 1 day ago"
			}
			return fmt.Sprintf("1 year and %d days ago", remDays)
		}
		if remDays == 1 {
			return fmt.Sprintf("%d years and 1 day ago", years)
		}
		return fmt.Sprintf("%d years and %d days ago", years, remDays)
	}
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}

// formatDaysAgo is an alias for formatAgeAgo (legacy name in tests).
func formatDaysAgo(age time.Duration) string {
	return formatAgeAgo(age)
}

// formatInDuration formats remaining time for min-release-age ("in 3 days", "in 2 hours").
func formatInDuration(remaining time.Duration) string {
	if remaining < 0 {
		remaining = 0
	}
	if remaining >= 14*24*time.Hour {
		weeks := int((remaining + 7*24*time.Hour - 1) / (7 * 24 * time.Hour))
		if weeks == 1 {
			return "1 week"
		}
		return fmt.Sprintf("%d weeks", weeks)
	}
	if remaining >= 24*time.Hour {
		days := int((remaining + 24*time.Hour - 1) / (24 * time.Hour))
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	if remaining >= time.Hour {
		hours := int((remaining + time.Hour - 1) / time.Hour)
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	minutes := int((remaining + time.Minute - 1) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	if minutes == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", minutes)
}

// formatInDays kept for tests; delegates to formatInDuration with day rounding.
func formatInDays(remaining time.Duration) string {
	if remaining < 0 {
		remaining = 0
	}
	days := int((remaining + 24*time.Hour - 1) / (24 * time.Hour))
	if days < 1 {
		days = 1
	}
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

func mergedAvailableColumn(disc discoveryDisplay) []string {
	out := make([]string, 0, len(disc.Eligible)+len(disc.EligibleSoon))
	out = append(out, disc.Eligible...)
	out = append(out, disc.EligibleSoon...)
	if len(out) == 0 && cfg.Flags.MinReleaseAge <= 0 {
		return disc.Available
	}
	return out
}
