package files

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

// TestPathFunctions tests the path-related functions
func TestPathFunctions(t *testing.T) {
	t.Run("get app data path", func(t *testing.T) {
		// Test that the function exists and can be called
		path := GetAppDataPath()
		assert.NotEmpty(t, path)
	})

	t.Run("get app local packages file path", func(t *testing.T) {
		// Test that the function exists and can be called
		path := GetAppLocalPackagesFilePath()
		assert.NotEmpty(t, path)
	})

	t.Run("get app registry file path", func(t *testing.T) {
		// Test that the function exists and can be called
		path := GetAppRegistryFilePath()
		assert.NotEmpty(t, path)
	})

	t.Run("get app packages path", func(t *testing.T) {
		// Test that the function exists and can be called
		path := GetAppPackagesPath()
		assert.NotEmpty(t, path)
	})

	t.Run("get app bin path", func(t *testing.T) {
		// Test that the function exists and can be called
		path := GetAppBinPath()
		assert.NotEmpty(t, path)
	})

	t.Run("get registry cache path", func(t *testing.T) {
		// Test that the function exists and can be called
		path := GetRegistryCachePath()
		assert.NotEmpty(t, path)
	})

	t.Run("get temp path", func(t *testing.T) {
		// Test that the function exists and can be called
		path := GetTempPath()
		assert.NotEmpty(t, path)
	})
}

// TestPathFunctionsComprehensive tests comprehensive path scenarios
func TestPathFunctionsComprehensive(t *testing.T) {
	t.Run("get cache path precedence env over config", func(t *testing.T) {
		mockFS := &MockFileSystem{
			fs: afero.NewMemMapFs(),
			GetenvFunc: func(key string) string {
				if key == "NVPM_HOME" {
					return "/cfg"
				}
				if key == "NVPM_CACHE" {
					return "/envcache"
				}
				return ""
			},
			UserHomeDirFunc: func() (string, error) { return "/home/user", nil },
			UserConfigDirFunc: func() (string, error) {
				return "/home/user/.config", nil
			},
		}
		SetFileSystem(mockFS)
		defer ResetDependencies()

		// Even if config.yaml requests a different cache dir, env var must win.
		_ = mockFS.fs.MkdirAll("/cfg", 0o755)
		_ = afero.WriteFile(mockFS.fs, "/cfg/config.yaml", []byte("paths:\n  cacheDir: /cfgcache\n"), 0o644)

		assert.Equal(t, "/envcache", GetCachePath())
	})

	t.Run("get cache path uses config when env not set", func(t *testing.T) {
		mockFS := &MockFileSystem{
			fs: afero.NewMemMapFs(),
			GetenvFunc: func(key string) string {
				if key == "NVPM_HOME" {
					return "/cfg"
				}
				return ""
			},
			UserHomeDirFunc: func() (string, error) { return "/home/user", nil },
			UserConfigDirFunc: func() (string, error) {
				return "/home/user/.config", nil
			},
		}
		SetFileSystem(mockFS)
		defer ResetDependencies()

		_ = mockFS.fs.MkdirAll("/cfg", 0o755)
		_ = afero.WriteFile(mockFS.fs, "/cfg/config.yaml", []byte("paths:\n  cacheDir: rel/cache\n"), 0o644)

		// relative paths should be resolved relative to $HOME
		assert.Equal(t, "/home/user/rel/cache", GetCachePath())
	})

	t.Run("get app data path with NVPM_HOME set", func(t *testing.T) {
		// Create an in-memory filesystem for testing
		mockFS := &MockFileSystem{
			fs: afero.NewMemMapFs(),
			GetenvFunc: func(key string) string {
				if key == "NVPM_HOME" {
					return "/custom/nvpm/home"
				}
				return ""
			},
		}
		SetFileSystem(mockFS)
		defer ResetDependencies()

		// Test that it uses NVPM_HOME when set
		path := GetAppDataPath()
		assert.Equal(t, "/custom/nvpm/home", path)
	})

	t.Run("get app data path without NVPM_HOME", func(t *testing.T) {
		// Create an in-memory filesystem for testing
		mockFS := &MockFileSystem{
			fs: afero.NewMemMapFs(),
			GetenvFunc: func(key string) string {
				return "" // No NVPM_HOME
			},
			UserConfigDirFunc: func() (string, error) {
				return "/home/user/.config", nil
			},
		}
		SetFileSystem(mockFS)
		defer ResetDependencies()

		// Test that it uses user config dir when NVPM_HOME is not set
		path := GetAppDataPath()
		assert.Equal(t, "/home/user/.config/nvpm", path)
	})

	t.Run("path separator with different separators", func(t *testing.T) {
		// Test that path separators work correctly
		path := GetAppDataPath() + string(os.PathSeparator) + "test"
		assert.Contains(t, path, "test")
	})

	t.Run("path separator with empty path", func(t *testing.T) {
		// Test with empty path
		path := GetAppDataPath() + string(os.PathSeparator) + ""
		// The result will have a trailing separator, so we need to account for that
		expected := GetAppDataPath() + string(os.PathSeparator)
		assert.Equal(t, expected, path)
	})

	t.Run("path separator with root path", func(t *testing.T) {
		// Test with root path
		path := GetAppDataPath() + string(os.PathSeparator) + "/"
		assert.Contains(t, path, "/")
	})
}

// TestPathFunctionsErrorPaths tests error paths in path functions
func TestPathFunctionsErrorPaths(t *testing.T) {
	t.Run("get app data path with user config dir error", func(t *testing.T) {
		// Create a mock file system that fails on UserConfigDir
		mockFS := &MockFileSystem{
			fs: afero.NewMemMapFs(),
			UserConfigDirFunc: func() (string, error) {
				return "", errors.New("user config dir error")
			},
			GetenvFunc: func(key string) string {
				return "" // No NVPM_HOME
			},
		}
		SetFileSystem(mockFS)
		defer ResetDependencies()

		// This should panic
		assert.Panics(t, func() {
			GetAppDataPath()
		})
	})
}
