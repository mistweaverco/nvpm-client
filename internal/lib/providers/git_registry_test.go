package providers

import (
	"testing"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/config"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveGitLatestFromRegistryPrefersBranchWhenTagStale(t *testing.T) {
	now := time.Now()
	tagDate := now.Add(-90 * 24 * time.Hour)
	branchDate := now.Add(-1 * 24 * time.Hour)

	item := registry_parser.RegistryItem{
		Version: "v1.0.0",
		Source:  registry_parser.RegistryItemSource{ID: "github:o/plugin"},
		Git: &registry_parser.RegistryItemGit{
			Refs: []registry_parser.RegistryItemGitRef{
				{Ref: "v1.0.0", Kind: "tag", Commit: "aaa", CommitDateUnix: tagDate.Unix()},
				{Ref: "main", Kind: "branch", Commit: "bbb", CommitDateUnix: branchDate.Unix()},
			},
		},
	}
	policy := PreferBranchPolicy{
		Branches: []string{"main", "master"},
		Kind:     config.PreferBranchWhenReleaseAgeGap,
		Gap:      60 * 24 * time.Hour,
	}

	result, decision, ok := ResolveGitLatestFromRegistry(item, policy)
	require.True(t, ok)
	assert.True(t, decision.UseBranch)
	assert.Equal(t, "main", result.Version)
	assert.Equal(t, "bbb", result.Commit)
	assert.Equal(t, "v1.0.0", result.SupersededTag)
}

func TestResolveGitLatestFromRegistryUsesTagWhenFresh(t *testing.T) {
	now := time.Now()
	tagDate := now.Add(-7 * 24 * time.Hour)
	branchDate := now.Add(-1 * 24 * time.Hour)

	item := registry_parser.RegistryItem{
		Version: "v1.0.0",
		Source:  registry_parser.RegistryItemSource{ID: "github:o/plugin"},
		Git: &registry_parser.RegistryItemGit{
			Refs: []registry_parser.RegistryItemGitRef{
				{Ref: "v1.0.0", Kind: "tag", Commit: "aaa", CommitDateUnix: tagDate.Unix()},
				{Ref: "main", Kind: "branch", Commit: "bbb", CommitDateUnix: branchDate.Unix()},
			},
		},
	}
	policy := PreferBranchPolicy{
		Branches: []string{"main"},
		Kind:     config.PreferBranchWhenReleaseAgeGap,
		Gap:      60 * 24 * time.Hour,
	}

	result, decision, ok := ResolveGitLatestFromRegistry(item, policy)
	require.True(t, ok)
	assert.False(t, decision.UseBranch)
	assert.Equal(t, "v1.0.0", result.Version)
	assert.Equal(t, "aaa", result.Commit)
}

func TestLatestSemverTagRef(t *testing.T) {
	refs := []registry_parser.RegistryItemGitRef{
		{Ref: "v1.0.0", Kind: "tag"},
		{Ref: "v1.2.0", Kind: "tag"},
		{Ref: "main", Kind: "branch"},
	}
	best, ok := latestSemverTagRef(refs)
	require.True(t, ok)
	assert.Equal(t, "v1.2.0", best.Ref)
}
