package providers

import (
	"testing"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestDecidePreferBranchAlways(t *testing.T) {
	policy := PreferBranchPolicy{
		Branches: []string{"main"},
		Kind:     config.PreferBranchWhenAlways,
	}
	now := time.Now()
	d := DecidePreferBranch(policy, now, true, now.Add(-90*24*time.Hour), true, now)
	assert.True(t, d.UseBranch)
	assert.Equal(t, "always", d.Reason)

	d = DecidePreferBranch(policy, now, true, now, false, time.Time{})
	assert.False(t, d.UseBranch)
}

func TestDecidePreferBranchReleaseAgeGap(t *testing.T) {
	policy := PreferBranchPolicy{
		Branches: []string{"main"},
		Kind:     config.PreferBranchWhenReleaseAgeGap,
		Gap:      60 * 24 * time.Hour,
	}
	now := time.Now()
	tagOld := now.Add(-90 * 24 * time.Hour)
	branchNew := now.Add(-1 * 24 * time.Hour)

	d := DecidePreferBranch(policy, now, true, tagOld, true, branchNew)
	assert.True(t, d.UseBranch)
	assert.Equal(t, "release-age-gap", d.Reason)

	tagFresh := now.Add(-10 * 24 * time.Hour)
	d = DecidePreferBranch(policy, now, true, tagFresh, true, branchNew)
	assert.False(t, d.UseBranch)
	assert.Equal(t, "tag within gap", d.Reason)

	d = DecidePreferBranch(policy, now, true, tagOld, true, tagOld.Add(-time.Hour))
	assert.False(t, d.UseBranch)
	assert.Equal(t, "branch not newer than tag", d.Reason)

	d = DecidePreferBranch(policy, now, true, time.Time{}, true, branchNew)
	assert.False(t, d.UseBranch)
	assert.Equal(t, "missing upstream dates", d.Reason)

	d = DecidePreferBranch(policy, now, false, time.Time{}, true, branchNew)
	assert.True(t, d.UseBranch)
	assert.Equal(t, "no tag", d.Reason)
}
