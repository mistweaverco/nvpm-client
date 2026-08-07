package nvpm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatDiscoveredVersion(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	seen := now.Add(-5 * 24 * time.Hour)
	assert.Equal(t, "v1.2.3 (5 days ago)", formatDiscoveredVersion("v1.2.3", seen, now))
	assert.Equal(t, "abc1234 (1 day ago)", formatDiscoveredVersion("abc1234", now.Add(-24*time.Hour), now))
	assert.Equal(t, "v1.0.0 (0 days ago)", formatDiscoveredVersion("v1.0.0", now.Add(-time.Hour), now))
	assert.Equal(t, "v1.0.0", formatDiscoveredVersion("v1.0.0", time.Time{}, now))
}

func TestFormatPreferBranchDisplay(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	seen := now.Add(-0 * time.Hour)
	assert.Equal(t, "main (322c79d) (0 days ago)", formatGitRefWithCommitAge("main", "322c79dabcdef", seen, now))
	assert.Equal(t, "322c79d (0 days ago; v1.2.3 120 days ago)", formatPreferBranchDiscovered(
		"322c79d", seen, now, "v1.2.3", 120*24*time.Hour,
	))
	assert.Equal(t, "main (322c79d)", formatEligibleGitRef("main", "322c79dabcdef", 0))
	assert.Equal(t, "main (322c79d) in 7 days", formatEligibleGitRef("main", "322c79dabcdef", 7*24*time.Hour))
	assert.Equal(t, "7 days", formatInDays(7*24*time.Hour))
	assert.Equal(t, "1 day", formatInDays(time.Hour))
}

func TestIsPreferBranchRef(t *testing.T) {
	assert.True(t, isPreferBranchRef("main"))
	assert.True(t, isPreferBranchRef("master"))
	assert.False(t, isPreferBranchRef("v1.2.3"))
}
