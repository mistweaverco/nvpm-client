package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckProviderPrerequisites_GenericAlwaysAvailable(t *testing.T) {
	require.NoError(t, CheckProviderPrerequisites("generic"))
}

func TestCheckSourceIDPrerequisites_InvalidID(t *testing.T) {
	err := CheckSourceIDPrerequisites("not-a-valid-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid package id")
}

func TestPrerequisiteErrorContainsHelpfulLinks(t *testing.T) {
	spec, ok := prerequisiteByProvider("npm")
	require.True(t, ok)
	err := prerequisiteError(spec, "npm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NPM provider requires")
	assert.Contains(t, err.Error(), "nodejs.org")
	assert.Contains(t, err.Error(), "nvpm health")
}

func TestProviderPrerequisiteHelp(t *testing.T) {
	help := ProviderPrerequisiteHelp("github")
	assert.Contains(t, help, "Git")
	assert.Contains(t, help, "git-scm.com")
}
