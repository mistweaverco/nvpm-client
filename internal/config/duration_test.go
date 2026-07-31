package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDuration(t *testing.T) {
	d, err := ParseDuration("7d")
	require.NoError(t, err)
	assert.Equal(t, 7*24*time.Hour, d)

	d, err = ParseDuration("60d")
	require.NoError(t, err)
	assert.Equal(t, 60*24*time.Hour, d)

	d, err = ParseDuration("1h30m")
	require.NoError(t, err)
	assert.Equal(t, 90*time.Minute, d)

	d, err = ParseDuration("0")
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), d)

	d, err = ParseDuration("24h")
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, d)

	_, err = ParseDuration("nope")
	require.Error(t, err)
}

func TestPreferBranchOverReleaseOrDefault(t *testing.T) {
	var fc FileConfig
	got := fc.PreferBranchOverReleaseOrDefault()
	def := DefaultPreferBranchOverRelease()
	assert.Equal(t, def, got)

	fc.Git.UpdateResolution.PrefersBranchOverRelease.When.Kind = "always"
	fc.Git.UpdateResolution.PrefersBranchOverRelease.Branches = []string{"trunk", "main"}
	got = fc.PreferBranchOverReleaseOrDefault()
	assert.Equal(t, PreferBranchWhenAlways, got.Kind)
	assert.Equal(t, []string{"trunk", "main"}, got.Branches)

	fc = FileConfig{}
	fc.Git.UpdateResolution.PrefersBranchOverRelease.When.Kind = "release-age-gap"
	fc.Git.UpdateResolution.PrefersBranchOverRelease.When.Gap = "30d"
	got = fc.PreferBranchOverReleaseOrDefault()
	assert.Equal(t, PreferBranchWhenReleaseAgeGap, got.Kind)
	assert.Equal(t, 30*24*time.Hour, got.Gap)
	assert.Equal(t, []string{"main", "master"}, got.Branches)
}
