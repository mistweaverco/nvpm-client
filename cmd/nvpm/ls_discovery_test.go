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
			GetLatestVersionsFunc: func(string) (string, string) { return "9.0.0", "9.1.0-rc.1" },
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
	_, ok = db.FirstSeenUnix["npm:eslint@9.1.0-rc.1"]
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

func TestNonRegistryDiscoveryOnRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NVPM_HOME", home)
	_ = files.GetAppDataPath()

	cfg.Flags.MinReleaseAge = 7 * 24 * time.Hour
	t.Cleanup(func() { cfg.Flags.MinReleaseAge = 0 })

	providers.SetDiscoveryWritesEnabled(true)
	t.Cleanup(func() { providers.SetDiscoveryWritesEnabled(true) })

	oldFn := providers.SetDiscoverGitRemoteLatestForTest(func(sourceID, installedVersion string) (providers.GitRemoteLatestResult, error) {
		return providers.GitRemoteLatestResult{
			Version: "v3.0.0",
			Commit:  "ffffffffffffffffffffffffffffffffffffffff",
		}, nil
	})
	t.Cleanup(oldFn)

	download := func() (bool, error) { return true, nil }
	svc := NewListServiceWithDependencies(
		&MockLocalPackagesProvider{
			GetDataFunc: func(bool) local_packages_parser.LocalPackageRoot {
				return local_packages_parser.LocalPackageRoot{Packages: []local_packages_parser.LocalPackageItem{
					{SourceID: "github:o/manual-plugin", Version: "main", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
					{SourceID: "npm:eslint", Version: "8.0.0"},
				}}
			},
		},
		&MockRegistryProvider{
			GetLatestVersionsFunc: func(sourceID string) (string, string) {
				if sourceID == "npm:eslint" {
					return "9.0.0", ""
				}
				return "", ""
			},
		},
		&MockUpdateChecker{},
		&MockFileDownloader{
			DownloadAndUnzipRegistryFunc:      download,
			DownloadAndUnzipRegistryQuietFunc: download,
		},
	)

	runListQuiet(t, func() {
		svc.ListInstalledPackages(ListQueryOptions{})
	})

	entry, ok, err := providers.GetRemoteLatest("github:o/manual-plugin")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "v3.0.0", entry.Version)

	_, ok, err = providers.GetRemoteLatest("npm:eslint")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestNonRegistryDiscoverySkippedWhenWarm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NVPM_HOME", home)
	_ = files.GetAppDataPath()

	cfg.Flags.MinReleaseAge = 7 * 24 * time.Hour
	t.Cleanup(func() { cfg.Flags.MinReleaseAge = 0 })

	providers.SetDiscoveryWritesEnabled(true)
	t.Cleanup(func() { providers.SetDiscoveryWritesEnabled(true) })

	called := false
	oldFn := providers.SetDiscoverGitRemoteLatestForTest(func(sourceID, installedVersion string) (providers.GitRemoteLatestResult, error) {
		called = true
		return providers.GitRemoteLatestResult{
			Version: "v3.0.0",
			Commit:  "ffffffffffffffffffffffffffffffffffffffff",
		}, nil
	})
	t.Cleanup(oldFn)

	download := func() (bool, error) { return false, nil }
	svc := NewListServiceWithDependencies(
		&MockLocalPackagesProvider{
			GetDataFunc: func(bool) local_packages_parser.LocalPackageRoot {
				return local_packages_parser.LocalPackageRoot{Packages: []local_packages_parser.LocalPackageItem{
					{SourceID: "github:o/manual-plugin", Version: "main", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				}}
			},
		},
		&MockRegistryProvider{},
		&MockUpdateChecker{},
		&MockFileDownloader{
			DownloadAndUnzipRegistryFunc:      download,
			DownloadAndUnzipRegistryQuietFunc: download,
		},
	)

	runListQuiet(t, func() {
		svc.ListInstalledPackages(ListQueryOptions{})
	})

	assert.False(t, called)
}

func TestCheckUpdateAvailabilityUsesRemoteLatestCommit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NVPM_HOME", home)
	_ = files.GetAppDataPath()

	providers.SetDiscoveryWritesEnabled(true)
	t.Cleanup(func() { providers.SetDiscoveryWritesEnabled(true) })

	require.NoError(t, providers.SetRemoteLatest("github:o/manual", providers.RemoteLatestEntry{
		Version: "main",
		Commit:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}))

	svc := NewListServiceWithDependencies(
		&MockLocalPackagesProvider{},
		&MockRegistryProvider{},
		&MockUpdateChecker{
			CheckIfUpdateIsAvailableFunc: func(currentVersion, latestVersion string) (bool, string) {
				t.Fatal("semver check should not run when commits differ")
				return false, ""
			},
		},
		&MockFileDownloader{},
	)

	info, hasUpdate := svc.checkUpdateAvailability(
		"github:o/manual",
		"main",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	assert.True(t, hasUpdate)
	assert.Contains(t, info, "Update available")

	info, hasUpdate = svc.checkUpdateAvailability(
		"github:o/manual",
		"main",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	)
	assert.False(t, hasUpdate)
	assert.Contains(t, info, "Up to date")
}

func TestDiscoveryDisplayPreferBranchSupersededTag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NVPM_HOME", home)
	_ = files.GetAppDataPath()

	providers.SetDiscoveryWritesEnabled(true)
	t.Cleanup(func() { providers.SetDiscoveryWritesEnabled(true) })

	now := time.Now()
	tagUnix := now.Add(-290 * 24 * time.Hour).Unix()
	require.NoError(t, providers.SetRemoteLatest("github:o/manual", providers.RemoteLatestEntry{
		Version:          "main",
		Commit:           "dddddddddddddddddddddddddddddddddddddddd",
		SupersededTag:    "v1.2.3",
		SupersededCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		SupersededUnix:   tagUnix,
	}))
	require.NoError(t, providers.RecordDiscoveryBatch([]providers.DiscoveryPair{{
		SourceID: "github:o/manual",
		Version:  "main",
		Commit:   "dddddddddddddddddddddddddddddddddddddddd",
	}}))

	// Backdate first-seen to 2 days ago.
	dbPath := filepath.Join(files.GetAppDataPath(), "discovery.json")
	b, err := os.ReadFile(dbPath)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	firstSeen, ok := raw["first_seen_unix"].(map[string]any)
	require.True(t, ok)
	key := "github:o/manual@main+dddddddddddddddddddddddddddddddddddddddd"
	firstSeen[key] = float64(now.Add(-2 * 24 * time.Hour).Unix())
	out, err := json.MarshalIndent(raw, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dbPath, out, 0644))

	svc := NewListServiceWithDependencies(
		&MockLocalPackagesProvider{},
		&MockRegistryProvider{
			GetLatestVersionsFunc: func(string) (string, string) { return "", "" },
		},
		&MockUpdateChecker{},
		&MockFileDownloader{},
	)

	disc := svc.discoveryDisplayForInstalled(
		"github:o/manual",
		"main",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	require.Len(t, disc.Available, 1)
	assert.Equal(t, "main (ddddddd) (2 days ago)", disc.Available[0])
	require.Len(t, disc.Discovered, 1)
	assert.Equal(t, "ddddddd (2 days ago; v1.2.3 290 days ago)", disc.Discovered[0])
	require.Len(t, disc.Eligible, 1)
	assert.Equal(t, "main (ddddddd)", disc.Eligible[0])
}

func TestDiscoveryDisplayPreferBranchEligibleSoon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NVPM_HOME", home)
	_ = files.GetAppDataPath()

	providers.SetDiscoveryWritesEnabled(true)
	t.Cleanup(func() { providers.SetDiscoveryWritesEnabled(true) })

	cfg.Flags.MinReleaseAge = 7 * 24 * time.Hour
	t.Cleanup(func() { cfg.Flags.MinReleaseAge = 0 })

	now := time.Now()
	tagUnix := now.Add(-120 * 24 * time.Hour).Unix()
	require.NoError(t, providers.SetRemoteLatest("github:o/manual", providers.RemoteLatestEntry{
		Version:          "main",
		Commit:           "322c79dfffffffffffffffffffffffffffffff",
		SupersededTag:    "v1.2.3",
		SupersededCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		SupersededUnix:   tagUnix,
	}))
	require.NoError(t, providers.RecordDiscoveryBatch([]providers.DiscoveryPair{{
		SourceID: "github:o/manual",
		Version:  "main",
		Commit:   "322c79dfffffffffffffffffffffffffffffff",
	}}))

	svc := NewListServiceWithDependencies(
		&MockLocalPackagesProvider{},
		&MockRegistryProvider{
			GetLatestVersionsFunc: func(string) (string, string) { return "", "" },
		},
		&MockUpdateChecker{},
		&MockFileDownloader{},
	)

	disc := svc.discoveryDisplayForInstalled(
		"github:o/manual",
		"main",
		"322c79cfffffffffffffffffffffffffffffff",
	)
	require.Len(t, disc.Available, 1)
	assert.Equal(t, "main (322c79d) (0 days ago)", disc.Available[0])
	require.Len(t, disc.Discovered, 1)
	assert.Equal(t, "322c79d (0 days ago; v1.2.3 120 days ago)", disc.Discovered[0])
	assert.Empty(t, disc.Eligible)
	require.Len(t, disc.EligibleSoon, 1)
	assert.Equal(t, "main (322c79d in 7 days)", disc.EligibleSoon[0])
}
