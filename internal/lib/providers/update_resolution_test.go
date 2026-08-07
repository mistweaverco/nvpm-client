package providers

import (
	"testing"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUpdateResolutionFlag(t *testing.T) {
	p, err := ParseUpdateResolutionFlag("always")
	require.NoError(t, err)
	assert.Equal(t, config.PreferBranchWhenAlways, p.Kind)

	p, err = ParseUpdateResolutionFlag("release-age-gap:30d")
	require.NoError(t, err)
	assert.Equal(t, config.PreferBranchWhenReleaseAgeGap, p.Kind)
	assert.Equal(t, 30*24*time.Hour, p.Gap)

	p, err = ParseUpdateResolutionFlag("branches:main,develop;release-age-gap:14d")
	require.NoError(t, err)
	assert.Equal(t, []string{"main", "develop"}, p.Branches)
	assert.Equal(t, 14*24*time.Hour, p.Gap)
}

func TestParseUpdateResolutionFlagInvalid(t *testing.T) {
	_, err := ParseUpdateResolutionFlag("nope")
	assert.Error(t, err)
}
