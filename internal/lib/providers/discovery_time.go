package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
)

type discoveryDB struct {
	// Key format: "<sourceId>@<version>"
	FirstSeenUnix map[string]int64 `json:"first_seen_unix"`
}

func discoveryDBPath() string {
	return filepath.Join(files.GetAppDataPath(), "discovery.json")
}

var discoveryWritesEnabled = true

func SetDiscoveryWritesEnabled(enabled bool) {
	discoveryWritesEnabled = enabled
}

func readDiscoveryDB() (discoveryDB, error) {
	if !discoveryWritesEnabled {
		return discoveryDB{FirstSeenUnix: map[string]int64{}}, nil
	}
	p := discoveryDBPath()
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return discoveryDB{FirstSeenUnix: map[string]int64{}}, nil
		}
		return discoveryDB{}, err
	}
	var db discoveryDB
	if err := json.Unmarshal(b, &db); err != nil {
		return discoveryDB{}, err
	}
	if db.FirstSeenUnix == nil {
		db.FirstSeenUnix = map[string]int64{}
	}
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
		id := p.SourceID
		ver := p.Version
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
