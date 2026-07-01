package config

import (
	"os"
	"time"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"gopkg.in/yaml.v3"
)

// FileConfig represents the optional user config.yaml file.
// It lives next to nvpm-lock.json in the NVPM config directory.
type FileConfig struct {
	Registry struct {
		URLs        []string `yaml:"urls"`
		CacheMaxAge string   `yaml:"cacheMaxAge"`
	} `yaml:"registry"`

	Paths struct {
		CacheDir string `yaml:"cacheDir"`
	} `yaml:"paths"`

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
	d, err := time.ParseDuration(fc.Registry.CacheMaxAge)
	if err != nil {
		return 0
	}
	if d < 0 {
		return 0
	}
	return d
}
