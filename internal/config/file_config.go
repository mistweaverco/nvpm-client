package config

import (
	"os"
	"strings"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"gopkg.in/yaml.v3"
)

// PreferBranchWhenKind selects when branch tips win over tags/releases.
type PreferBranchWhenKind string

const (
	PreferBranchWhenAlways        PreferBranchWhenKind = "always"
	PreferBranchWhenReleaseAgeGap PreferBranchWhenKind = "release-age-gap"
)

// PreferBranchOverRelease is the resolved policy for git update resolution.
type PreferBranchOverRelease struct {
	Branches []string
	Kind     PreferBranchWhenKind
	Gap      time.Duration
}

// DefaultPreferBranchOverRelease is the built-in default (even with no config.yaml).
func DefaultPreferBranchOverRelease() PreferBranchOverRelease {
	return PreferBranchOverRelease{
		Branches: []string{"main", "master"},
		Kind:     PreferBranchWhenReleaseAgeGap,
		Gap:      60 * 24 * time.Hour,
	}
}

// FileConfig represents the optional user config.yaml file.
// It lives next to nvpm-lock.json in the NVPM config directory.
type FileConfig struct {
	Registry struct {
		URLs          []string `yaml:"urls"`
		CacheMaxAge   string   `yaml:"cache-max-age"`
		MinReleaseAge string   `yaml:"min-release-age"`
	} `yaml:"registry"`

	Paths struct {
		CacheDir string `yaml:"cache-dir"`
	} `yaml:"paths"`

	Git struct {
		UpdateResolution struct {
			PrefersBranchOverRelease struct {
				Branches []string `yaml:"branches"`
				When     struct {
					Kind string `yaml:"kind"`
					Gap  string `yaml:"gap"`
				} `yaml:"when"`
			} `yaml:"prefers-branch-over-release"`
		} `yaml:"update-resolution"`
	} `yaml:"git"`

	UI struct {
		Color  string `yaml:"color"`
		Output string `yaml:"output"`
	} `yaml:"ui"`
}

func ConfigFilePath() string {
	return files.GetAppDataPath() + string(os.PathSeparator) + "config.yaml"
}

// LoadFileConfig reads config.yaml. If the file doesn't exist, it returns (zeroValue, false, nil).
func LoadFileConfig() (FileConfig, bool, error) {
	path := ConfigFilePath()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileConfig{}, false, nil
		}
		return FileConfig{}, false, err
	}

	var cfg FileConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return FileConfig{}, true, err
	}
	return cfg, true, nil
}

func (fc FileConfig) RegistryCacheMaxAgeOrZero() time.Duration {
	if fc.Registry.CacheMaxAge == "" {
		return 0
	}
	d, err := ParseDuration(fc.Registry.CacheMaxAge)
	if err != nil {
		return 0
	}
	if d < 0 {
		return 0
	}
	return d
}

func (fc FileConfig) RegistryMinReleaseAgeOrZero() time.Duration {
	if fc.Registry.MinReleaseAge == "" {
		return 0
	}
	d, err := ParseDuration(fc.Registry.MinReleaseAge)
	if err != nil {
		return 0
	}
	if d < 0 {
		return 0
	}
	return d
}

// PreferBranchOverReleaseOrDefault returns the configured policy, or the built-in default
// when the section is absent / incomplete.
func (fc FileConfig) PreferBranchOverReleaseOrDefault() PreferBranchOverRelease {
	def := DefaultPreferBranchOverRelease()
	p := fc.Git.UpdateResolution.PrefersBranchOverRelease

	kind := PreferBranchWhenKind(strings.TrimSpace(p.When.Kind))
	if kind == "" {
		return def
	}
	if kind != PreferBranchWhenAlways && kind != PreferBranchWhenReleaseAgeGap {
		return def
	}

	out := PreferBranchOverRelease{
		Branches: def.Branches,
		Kind:     kind,
		Gap:      def.Gap,
	}
	if len(p.Branches) > 0 {
		branches := make([]string, 0, len(p.Branches))
		for _, b := range p.Branches {
			b = strings.TrimSpace(b)
			if b != "" {
				branches = append(branches, b)
			}
		}
		if len(branches) > 0 {
			out.Branches = branches
		}
	}
	if kind == PreferBranchWhenReleaseAgeGap {
		if gapStr := strings.TrimSpace(p.When.Gap); gapStr != "" {
			if d, err := ParseDuration(gapStr); err == nil && d >= 0 {
				out.Gap = d
			}
		}
	}
	return out
}
