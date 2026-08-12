package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
)

// RemoteLatestEntry caches the last remotely discovered latest version for a
// package that is not present in the local registry snapshot.
type RemoteLatestEntry struct {
	Version     string `json:"version"`
	Commit      string `json:"commit,omitempty"`
	CheckedUnix int64  `json:"checked_unix"`
	// SupersededTag is the newest semver tag that was skipped when prefer-branch-over-release
	// chose a branch tip (release-age-gap). Empty when no tag was superseded.
	SupersededTag string `json:"superseded_tag,omitempty"`
	// SupersededCommit is the commit SHA of SupersededTag.
	SupersededCommit string `json:"superseded_commit,omitempty"`
	// SupersededUnix is the upstream commit date of SupersededTag (unix seconds).
	SupersededUnix int64 `json:"superseded_unix,omitempty"`
}

// HasSupersededTag reports whether prefer-branch recorded a skipped stale tag.
func (e RemoteLatestEntry) HasSupersededTag() bool {
	return strings.TrimSpace(e.SupersededTag) != ""
}

type discoveryDB struct {
	// Key format: "<sourceId>@<version>" where version is a registry semver for immutable
	// providers, or "tag+commit" / "commit" for git-hosted packages.
	FirstSeenUnix map[string]int64 `json:"first_seen_unix"`
	// RemoteLatest maps sourceID → last discovered remote latest (non-registry packages).
	RemoteLatest map[string]RemoteLatestEntry `json:"remote_latest,omitempty"`
}

func discoveryDBPath() string {
	return filepath.Join(files.GetAppDataPath(), "discovery.json")
}

var discoveryWritesEnabled = true

func SetDiscoveryWritesEnabled(enabled bool) {
	discoveryWritesEnabled = enabled
}

func emptyDiscoveryDB() discoveryDB {
	return discoveryDB{
		FirstSeenUnix: map[string]int64{},
		RemoteLatest:  map[string]RemoteLatestEntry{},
	}
}

func normalizeDiscoveryDB(db *discoveryDB) {
	if db.FirstSeenUnix == nil {
		db.FirstSeenUnix = map[string]int64{}
	}
	if db.RemoteLatest == nil {
		db.RemoteLatest = map[string]RemoteLatestEntry{}
	}
}

func readDiscoveryDB() (discoveryDB, error) {
	if !discoveryWritesEnabled {
		return emptyDiscoveryDB(), nil
	}
	p := discoveryDBPath()
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyDiscoveryDB(), nil
		}
		return discoveryDB{}, err
	}
	var db discoveryDB
	if err := json.Unmarshal(b, &db); err != nil {
		return discoveryDB{}, err
	}
	normalizeDiscoveryDB(&db)
	return db, nil
}

func writeDiscoveryDB(db discoveryDB) error {
	if !discoveryWritesEnabled {
		return nil
	}
	dir := files.GetAppDataPath()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".discovery.%d.json", time.Now().UnixNano()))
	b, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, discoveryDBPath())
}

func discoveryKey(sourceID, version string) string {
	return sourceID + "@" + version
}

type DiscoveryPair struct {
	SourceID string
	Version  string
	// Commit is the resolved git commit SHA when SourceID is git-hosted. Required for
	// batch recording; list commands pass it from the lockfile. Install/update resolves
	// commits via git ls-remote in discoveryVersionForEnforcement.
	Commit string
}

type DiscoveredVersion struct {
	Version   string    `json:"version"`
	FirstSeen time.Time `json:"first_seen"`
}

func RecordDiscovery(sourceID, version string) error {
	_, err := getOrSetFirstSeen(sourceID, version, time.Now())
	return err
}

func RecordDiscoveryBatch(pairs []DiscoveryPair) error {
	if !discoveryWritesEnabled {
		return nil
	}
	now := time.Now()
	db, err := readDiscoveryDB()
	if err != nil {
		return err
	}
	changed := false
	for _, p := range pairs {
		enriched, err := enrichDiscoveryPair(p)
		if err != nil {
			continue
		}
		id := enriched.SourceID
		ver := enriched.Version
		if id == "" || ver == "" || ver == "latest" {
			continue
		}
		k := discoveryKey(id, ver)
		if unix, ok := db.FirstSeenUnix[k]; ok && unix > 0 {
			continue
		}
		db.FirstSeenUnix[k] = now.Unix()
		changed = true
	}
	if !changed {
		return nil
	}
	return writeDiscoveryDB(db)
}

// GetFirstSeen returns the discovery time for (sourceID, version) if present.
// Unlike getOrSetFirstSeen, this is a read-only query and does not write the DB.
func GetFirstSeen(sourceID, version string) (time.Time, bool, error) {
	db, err := readDiscoveryDB()
	if err != nil {
		return time.Time{}, false, err
	}
	k := discoveryKey(sourceID, version)
	if unix, ok := db.FirstSeenUnix[k]; ok && unix > 0 {
		return time.Unix(unix, 0), true, nil
	}
	return time.Time{}, false, nil
}

// ListDiscoveredVersions returns all recorded versions for a given sourceID,
// sorted by most-recent first.
func ListDiscoveredVersions(sourceID string) ([]DiscoveredVersion, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, nil
	}
	db, err := readDiscoveryDB()
	if err != nil {
		return nil, err
	}
	prefix := sourceID + "@"
	out := make([]DiscoveredVersion, 0, 16)
	for k, unix := range db.FirstSeenUnix {
		if unix <= 0 || !strings.HasPrefix(k, prefix) {
			continue
		}
		ver := strings.TrimPrefix(k, prefix)
		if ver == "" {
			continue
		}
		out = append(out, DiscoveredVersion{
			Version:   ver,
			FirstSeen: time.Unix(unix, 0),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].FirstSeen.After(out[j].FirstSeen)
	})
	return out, nil
}

// getOrSetFirstSeen returns the first discovery time for (sourceID, version),
// storing "now" if it's not recorded yet.
func getOrSetFirstSeen(sourceID, version string, now time.Time) (time.Time, error) {
	db, err := readDiscoveryDB()
	if err != nil {
		return time.Time{}, err
	}
	k := discoveryKey(sourceID, version)
	if unix, ok := db.FirstSeenUnix[k]; ok && unix > 0 {
		return time.Unix(unix, 0), nil
	}
	db.FirstSeenUnix[k] = now.Unix()
	if err := writeDiscoveryDB(db); err != nil {
		return time.Time{}, err
	}
	return now, nil
}

// GetRemoteLatest returns the cached remote-latest entry for sourceID, if any.
func GetRemoteLatest(sourceID string) (RemoteLatestEntry, bool, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return RemoteLatestEntry{}, false, nil
	}
	db, err := readDiscoveryDB()
	if err != nil {
		return RemoteLatestEntry{}, false, err
	}
	entry, ok := db.RemoteLatest[sourceID]
	if !ok || strings.TrimSpace(entry.Version) == "" {
		return RemoteLatestEntry{}, false, nil
	}
	return entry, true, nil
}

// SetRemoteLatest stores (or replaces) the remote-latest cache entry for sourceID.
func SetRemoteLatest(sourceID string, entry RemoteLatestEntry) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || strings.TrimSpace(entry.Version) == "" {
		return nil
	}
	if !discoveryWritesEnabled {
		return nil
	}
	db, err := readDiscoveryDB()
	if err != nil {
		return err
	}
	if entry.CheckedUnix <= 0 {
		entry.CheckedUnix = time.Now().Unix()
	}
	db.RemoteLatest[sourceID] = RemoteLatestEntry{
		Version:          strings.TrimSpace(entry.Version),
		Commit:           strings.TrimSpace(entry.Commit),
		CheckedUnix:      entry.CheckedUnix,
		SupersededTag:    strings.TrimSpace(entry.SupersededTag),
		SupersededCommit: strings.TrimSpace(entry.SupersededCommit),
		SupersededUnix:   entry.SupersededUnix,
	}
	return writeDiscoveryDB(db)
}

// HasGitCommitUpdate reports whether installed and remote commits differ (both non-empty).
func HasGitCommitUpdate(installedCommit, remoteCommit string) bool {
	installedCommit = strings.TrimSpace(installedCommit)
	remoteCommit = strings.TrimSpace(remoteCommit)
	if installedCommit == "" || remoteCommit == "" {
		return false
	}
	if strings.EqualFold(installedCommit, remoteCommit) {
		return false
	}
	// Treat short/full SHA prefixes as equal.
	a := strings.ToLower(installedCommit)
	b := strings.ToLower(remoteCommit)
	if len(a) >= 7 && len(b) >= 7 && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a)) {
		return false
	}
	return true
}

// GitCommitStillNeedsUpdate reports whether a registry/remote commit mismatch still
// means the package needs updating. Local-only (no network): if remote_latest matches
// the installed commit and the registry tip is a previously discovered tip for this ref
// (e.g. a force-moved tag we already accepted), it is not an update.
func GitCommitStillNeedsUpdate(sourceID, ref, installedCommit, remoteCommit string) bool {
	if !HasGitCommitUpdate(installedCommit, remoteCommit) {
		return false
	}
	entry, ok, err := GetRemoteLatest(sourceID)
	if err != nil || !ok {
		return true
	}
	ref = strings.TrimSpace(ref)
	if ref != "" && strings.TrimSpace(entry.Version) != "" && !strings.EqualFold(entry.Version, ref) {
		return true
	}
	if HasGitCommitUpdate(installedCommit, entry.Commit) {
		return true
	}
	// Install matches remote_latest. Only ignore the registry tip when it is an older
	// tip we already recorded for this ref (stale metadata after accepting a move).
	prevs, err := DiscoveredCommitsForRef(sourceID, ref)
	if err != nil {
		return true
	}
	for _, prev := range prevs {
		if gitCommitsEqual(prev, remoteCommit) {
			return false
		}
	}
	return true
}

// RefreshRemoteLatestAfterInstall updates remote_latest when we just installed the
// cached "latest" ref (or when no cache exists yet), so commit tips stay in sync.
func RefreshRemoteLatestAfterInstall(sourceID, version, commit string) {
	sourceID = strings.TrimSpace(sourceID)
	version = strings.TrimSpace(version)
	commit = strings.TrimSpace(commit)
	if !IsGitHostedSourceID(sourceID) || version == "" || commit == "" {
		return
	}
	entry, ok, err := GetRemoteLatest(sourceID)
	if err != nil {
		return
	}
	if ok && strings.TrimSpace(entry.Version) != "" && !strings.EqualFold(entry.Version, version) {
		// Installed an older/pinned ref; do not clobber the cached latest label.
		return
	}
	_ = SetRemoteLatest(sourceID, RemoteLatestEntry{
		Version: version,
		Commit:  commit,
	})
}
