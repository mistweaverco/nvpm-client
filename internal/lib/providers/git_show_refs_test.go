package providers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverGitRefSnapshotBranchesAndTag(t *testing.T) {
	oldShell := gitDiscoveryShellOutCapture
	oldDates := fetchGitCommitDateFn
	defer func() {
		gitDiscoveryShellOutCapture = oldShell
		fetchGitCommitDateFn = oldDates
	}()

	mainCommit := "1111111111111111111111111111111111111111"
	devCommit := "2222222222222222222222222222222222222222"
	tagCommit := "3333333333333333333333333333333333333333"
	mainTime := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	devTime := time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC)
	tagTime := time.Date(2025, 8, 1, 12, 0, 0, 0, time.UTC)

	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 2 && args[0] == "ls-remote" && args[1] == "--tags" {
			return 0, tagCommit + "\trefs/tags/v1.10.2\n", nil
		}
		if len(args) >= 3 && args[0] == "ls-remote" {
			ref := args[2]
			switch ref {
			case "refs/heads/main", "refs/heads/main^{}":
				return 0, mainCommit + "\trefs/heads/main\n", nil
			case "refs/heads/develop", "refs/heads/develop^{}":
				return 0, devCommit + "\trefs/heads/develop\n", nil
			case "refs/tags/v1.10.2^{}":
				return 0, tagCommit + "\trefs/tags/v1.10.2^{}\n", nil
			}
		}
		return 1, "", nil
	}

	fetchGitCommitDateFn = func(sourceID, commit string) (time.Time, error) {
		switch commit {
		case mainCommit:
			return mainTime, nil
		case devCommit:
			return devTime, nil
		case tagCommit:
			return tagTime, nil
		default:
			return time.Time{}, assert.AnError
		}
	}

	snaps, err := DiscoverGitRefSnapshot("github:saghen/blink.cmp", "v1.10.2", "")
	require.NoError(t, err)
	require.Len(t, snaps, 3)

	byRef := map[string]GitRefSnapshot{}
	for _, s := range snaps {
		byRef[s.Ref] = s
	}
	assert.Equal(t, "branch", byRef["main"].Kind)
	assert.Equal(t, mainCommit, byRef["main"].Commit)
	assert.Equal(t, mainTime.Unix(), byRef["main"].CommitDateUnix)
	assert.Equal(t, "branch", byRef["develop"].Kind)
	assert.Equal(t, devCommit, byRef["develop"].Commit)
	assert.Equal(t, "tag", byRef["v1.10.2"].Kind)
	assert.Equal(t, tagCommit, byRef["v1.10.2"].Commit)
}
