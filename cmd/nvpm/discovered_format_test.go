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

func TestIsPreferBranchRef(t *testing.T) {
	assert.True(t, isPreferBranchRef("main"))
	assert.True(t, isPreferBranchRef("master"))
	assert.False(t, isPreferBranchRef("v1.2.3"))
}
