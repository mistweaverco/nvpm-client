package providers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinReleaseAgeTooSoonErrorMessage(t *testing.T) {
	err := &MinReleaseAgeTooSoonError{
		SourceID:  "npm:eslint",
		Version:   "9.0.0",
		Age:       2 * 24 * time.Hour,
		Remaining: 5 * 24 * time.Hour,
	}
	msg := err.Error()
	assert.Contains(t, msg, "npm:eslint@9.0.0")
	assert.Contains(t, msg, "waiting for min-release-age")
	assert.Contains(t, msg, "available in 5 days")
	assert.Contains(t, msg, "--force")

	tooSoon, ok := AsMinReleaseAgeTooSoon(err)
	require.True(t, ok)
	assert.Equal(t, "npm:eslint", tooSoon.SourceID)
}

func TestFormatFriendlyDuration(t *testing.T) {
	assert.Equal(t, "1 minute", formatFriendlyDuration(30*time.Second))
	assert.Equal(t, "3 hours", formatFriendlyDuration(3*time.Hour))
	assert.Equal(t, "5 days", formatFriendlyDuration(5*24*time.Hour))
	assert.Equal(t, "2 weeks", formatFriendlyDuration(14*24*time.Hour))
}

func TestLastSkipClearsError(t *testing.T) {
	ClearLastError()
	SetLastError("boom")
	SetLastSkip("waiting")
	assert.Equal(t, "", TakeLastError())
	assert.Equal(t, "waiting", TakeLastSkip())
	assert.Equal(t, "", TakeLastSkip())
}
