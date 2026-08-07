package nvpm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatAgeAgoYears(t *testing.T) {
	assert.Equal(t, "1 year and 201 days ago", formatAgeAgo((365+201)*24*time.Hour))
	assert.Equal(t, "300 days ago", formatAgeAgo(300*24*time.Hour))
}

func TestFormatInDurationUnits(t *testing.T) {
	assert.Equal(t, "2 weeks", formatInDuration(14*24*time.Hour))
	assert.Equal(t, "3 hours", formatInDuration(3*time.Hour))
	assert.Equal(t, "45 minutes", formatInDuration(45*time.Minute))
}

func TestMergedAvailableColumn(t *testing.T) {
	old := cfg.Flags.MinReleaseAge
	cfg.Flags.MinReleaseAge = 7 * 24 * time.Hour
	defer func() { cfg.Flags.MinReleaseAge = old }()

	disc := discoveryDisplay{
		Eligible:     []string{"v2.0.0"},
		EligibleSoon: []string{"v2.1.0-rc (in 3 days)"},
	}
	assert.Equal(t, []string{"v2.0.0", "v2.1.0-rc (in 3 days)"}, mergedAvailableColumn(disc))
}
