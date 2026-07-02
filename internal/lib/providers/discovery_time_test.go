package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordDiscoveryBatchGitUsesTagPlusCommit(t *testing.T) {
	_ = withTempNvpmHome(t)

	err := RecordDiscoveryBatch([]DiscoveryPair{{
		SourceID: "github:o/r",
		Version:  "v1.0.0",
		Commit:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}})
	require.NoError(t, err)

	b, err := os.ReadFile(filepath.Join(files.GetAppDataPath(), "discovery.json"))
	require.NoError(t, err)
	var db discoveryDB
	require.NoError(t, json.Unmarshal(b, &db))
	_, ok := db.FirstSeenUnix["github:o/r@v1.0.0+aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]
	assert.True(t, ok)
}

func TestEnforceMinReleaseAgeGitDiscoveryKey(t *testing.T) {
	_ = withTempNvpmHome(t)
	old := gitDiscoveryShellOutCapture
	defer func() { gitDiscoveryShellOutCapture = old }()
	gitDiscoveryShellOutCapture = func(_ string, args []string, _ string, _ []string) (int, string, error) {
		if len(args) >= 3 && args[0] == "ls-remote" {
			return 0, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\trefs/tags/v3.0.0^{}\n", nil
		}
		return 1, "", nil
	}

	SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 7 * 24 * time.Hour})
	t.Cleanup(func() { SetMinReleaseAgePolicy(MinReleaseAgePolicy{MinAge: 0}) })

	err := enforceMinReleaseAge("github:o/r", "v3.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "github:o/r@v3.0.0+bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
}
