package nvpm

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func listDiscoveryTestService(refreshed bool) *ListService {
	download := func() (bool, error) { return refreshed, nil }
	return NewListServiceWithDependencies(
		&MockLocalPackagesProvider{
			GetDataFunc: func(bool) local_packages_parser.LocalPackageRoot {
				return local_packages_parser.LocalPackageRoot{Packages: []local_packages_parser.LocalPackageItem{
					{SourceID: "npm:eslint", Version: "8.0.0"},
				}}
			},
		},
		&MockRegistryProvider{
			GetLatestVersionsFunc: func(string) (string, string) { return "9.0.0", "" },
		},
		&MockUpdateChecker{},
		&MockFileDownloader{
			DownloadAndUnzipRegistryFunc:      download,
			DownloadAndUnzipRegistryQuietFunc: download,
		},
	)
}

func runListQuiet(t *testing.T, fn func()) {
	t.Helper()
	oldOut := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	showDiscoveryProgress = false
	showRegistryProgress = false
	t.Cleanup(func() {
		showDiscoveryProgress = true
		showRegistryProgress = true
	})
	fn()
	require.NoError(t, w.Close())
	os.Stdout = oldOut
	_, _ = io.Copy(io.Discard, r)
}

func TestRecordDiscoveryOnRegistryRefreshSkipsWarmCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NVPM_HOME", home)
	_ = files.GetAppDataPath()

	cfg.Flags.MinReleaseAge = 7 * 24 * time.Hour
	t.Cleanup(func() { cfg.Flags.MinReleaseAge = 0 })

	providers.SetDiscoveryWritesEnabled(true)
	t.Cleanup(func() { providers.SetDiscoveryWritesEnabled(true) })

	showDiscoveryProgress = false
	showRegistryProgress = false

	runListQuiet(t, func() {
		listDiscoveryTestService(false).ListInstalledPackages(ListQueryOptions{})
	})

	_, err := os.Stat(filepath.Join(files.GetAppDataPath(), "discovery.json"))
	assert.True(t, os.IsNotExist(err))
}

func TestRecordDiscoveryOnRegistryRefreshRecordsAfterRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NVPM_HOME", home)
	_ = files.GetAppDataPath()

	cfg.Flags.MinReleaseAge = 7 * 24 * time.Hour
	t.Cleanup(func() { cfg.Flags.MinReleaseAge = 0 })

	providers.SetDiscoveryWritesEnabled(true)
	t.Cleanup(func() { providers.SetDiscoveryWritesEnabled(true) })

	showDiscoveryProgress = false
	showRegistryProgress = false

	runListQuiet(t, func() {
		listDiscoveryTestService(true).ListInstalledPackages(ListQueryOptions{})
	})

	b, err := os.ReadFile(filepath.Join(files.GetAppDataPath(), "discovery.json"))
	require.NoError(t, err)
	var db struct {
		FirstSeenUnix map[string]int64 `json:"first_seen_unix"`
	}
	require.NoError(t, json.Unmarshal(b, &db))
	_, ok := db.FirstSeenUnix["npm:eslint@9.0.0"]
	assert.True(t, ok)
}

func TestDiscoveryPairsForRegistrySkipsGitHosted(t *testing.T) {
	ls := NewListServiceWithDependencies(
		&MockLocalPackagesProvider{},
		&MockRegistryProvider{},
		&MockUpdateChecker{},
		&MockFileDownloader{},
	)
	cfg.Flags.MinReleaseAge = time.Hour
	t.Cleanup(func() { cfg.Flags.MinReleaseAge = 0 })

	pairs := ls.discoveryPairsForRegistry([]registry_parser.RegistryItem{
		{Source: registry_parser.RegistryItemSource{ID: "github:o/r"}, Version: "v1.0.0"},
		{Source: registry_parser.RegistryItemSource{ID: "npm:eslint"}, Version: "9.0.0"},
	})
	require.Len(t, pairs, 1)
	assert.Equal(t, "npm:eslint", pairs[0].SourceID)
}
