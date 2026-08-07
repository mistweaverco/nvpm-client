package local_packages_parser

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLocalPackagesParser(t *testing.T) {
	t.Run("local package item structure", func(t *testing.T) {
		item := LocalPackageItem{
			SourceID: "pkg:npm/test-package",
			Version:  "1.0.0",
		}

		assert.Equal(t, "pkg:npm/test-package", item.SourceID)
		assert.Equal(t, "1.0.0", item.Version)
	})

	t.Run("local package root structure", func(t *testing.T) {
		root := LocalPackageRoot{
			Packages: []LocalPackageItem{
				{SourceID: "pkg:npm/test-package", Version: "1.0.0"},
				{SourceID: "pkg:pypi/black", Version: "2.0.0"},
			},
		}

		assert.Len(t, root.Packages, 2)
		assert.Equal(t, "pkg:npm/test-package", root.Packages[0].SourceID)
		assert.Equal(t, "1.0.0", root.Packages[0].Version)
		assert.Equal(t, "pkg:pypi/black", root.Packages[1].SourceID)
		assert.Equal(t, "2.0.0", root.Packages[1].Version)
	})
}

func TestLocalPackagesParserWithMock(t *testing.T) {
	t.Run("new parser creation", func(t *testing.T) {
		parser := New()
		assert.NotNil(t, parser)
		assert.NotNil(t, parser.fileManager)
	})

	t.Run("add local package marshal error bubbles up", func(t *testing.T) {
		// swap marshalIndent
		old := marshalIndent
		marshalIndent = func(v any, prefix, indent string) ([]byte, error) {
			return nil, errors.New("marshal failed")
		}
		defer func() { marshalIndent = old }()

		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return false },
		}
		parser := NewWithFileManager(mockFileManager)
		err := parser.AddLocalPackage("pkg:npm/a", "1")
		assert.Error(t, err)
	})

	t.Run("get data with read error returns empty", func(t *testing.T) {
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string {
				return "/mock/path/local-packages.json"
			},
			FileExistsFunc: func(path string) bool { return true },
			ReadFileFunc:   func(path string) ([]byte, error) { return nil, errors.New("boom") },
		}

		parser := NewWithFileManager(mockFileManager)
		result := parser.GetData(false)
		assert.Empty(t, result.Packages)
	})

	t.Run("remove local package marshal error bubbles up", func(t *testing.T) {
		old := marshalIndent
		marshalIndent = func(v any, prefix, indent string) ([]byte, error) { return nil, errors.New("marshal failed") }
		defer func() { marshalIndent = old }()

		existingData := LocalPackageRoot{Packages: []LocalPackageItem{{SourceID: "pkg:npm/x", Version: "1.0.0"}}}
		jsonData, _ := json.Marshal(existingData)
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return true },
			ReadFileFunc:                    func(path string) ([]byte, error) { return jsonData, nil },
		}
		parser := NewWithFileManager(mockFileManager)
		err := parser.RemoveLocalPackage("pkg:npm/x")
		assert.Error(t, err)
	})

	t.Run("get data with parse error returns empty", func(t *testing.T) {
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return true },
			ReadFileFunc:                    func(path string) ([]byte, error) { return []byte("not-json"), nil },
		}

		parser := NewWithFileManager(mockFileManager)
		result := parser.GetData(false)
		assert.Empty(t, result.Packages)
	})

	t.Run("new parser with custom file manager", func(t *testing.T) {
		mockFileManager := &MockFileManager{}
		parser := NewWithFileManager(mockFileManager)
		assert.NotNil(t, parser)
		assert.Equal(t, mockFileManager, parser.fileManager)
	})

	t.Run("add local package write error bubbles up", func(t *testing.T) {
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return false },
			WriteFileFunc:                   func(path string, data []byte, perm uint32) error { return errors.New("write failed") },
		}
		parser := NewWithFileManager(mockFileManager)
		err := parser.AddLocalPackage("pkg:npm/new", "0.1.0")
		assert.Error(t, err)
	})

	t.Run("get data with empty file", func(t *testing.T) {
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string {
				return "/mock/path/local-packages.json"
			},
			FileExistsFunc: func(path string) bool {
				return false
			},
		}

		parser := NewWithFileManager(mockFileManager)
		result := parser.GetData(false)

		assert.Empty(t, result.Packages)
	})

	t.Run("remove local package write error bubbles up", func(t *testing.T) {
		existingData := LocalPackageRoot{Packages: []LocalPackageItem{{SourceID: "pkg:npm/x", Version: "1.0.0"}}}
		jsonData, _ := json.Marshal(existingData)
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return true },
			ReadFileFunc:                    func(path string) ([]byte, error) { return jsonData, nil },
			WriteFileFunc:                   func(path string, data []byte, perm uint32) error { return errors.New("write failed") },
		}
		parser := NewWithFileManager(mockFileManager)
		err := parser.RemoveLocalPackage("pkg:npm/x")
		assert.Error(t, err)
	})

	t.Run("remove non-existent package writes unchanged data", func(t *testing.T) {
		existingData := LocalPackageRoot{Packages: []LocalPackageItem{{SourceID: "pkg:npm/keep", Version: "1.0.0"}}}
		jsonData, _ := json.Marshal(existingData)
		var written []byte
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return true },
			ReadFileFunc:                    func(path string) ([]byte, error) { return jsonData, nil },
			WriteFileFunc:                   func(path string, data []byte, perm uint32) error { written = data; return nil },
		}
		parser := NewWithFileManager(mockFileManager)
		err := parser.RemoveLocalPackage("pkg:npm/missing")
		assert.NoError(t, err)
		var saved LocalPackageRoot
		_ = json.Unmarshal(written, &saved)
		// IDs are normalized to new format, so pkg:npm/keep becomes npm:keep
		expectedNormalized := LocalPackageRoot{
			Packages: []LocalPackageItem{{SourceID: "npm:keep", Version: "1.0.0"}},
			Schema:   lockSchemaURL,
		}
		assert.Equal(t, expectedNormalized, saved)
	})

	t.Run("get data with existing file", func(t *testing.T) {
		expectedData := LocalPackageRoot{
			Packages: []LocalPackageItem{
				{SourceID: "pkg:npm/test-package", Version: "1.0.0"},
				{SourceID: "pkg:pypi/black", Version: "2.0.0"},
			},
		}

		jsonData, _ := json.Marshal(expectedData)

		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string {
				return "/mock/path/local-packages.json"
			},
			FileExistsFunc: func(path string) bool {
				return true
			},
			ReadFileFunc: func(path string) ([]byte, error) {
				return jsonData, nil
			},
		}

		parser := NewWithFileManager(mockFileManager)
		result := parser.GetData(false)

		assert.Len(t, result.Packages, 2)
		// GetData normalizes IDs from pkg:provider/pkg to provider:pkg
		assert.Equal(t, "npm:test-package", result.Packages[0].SourceID)
		assert.Equal(t, "1.0.0", result.Packages[0].Version)
		assert.Equal(t, "pypi:black", result.Packages[1].SourceID)
		assert.Equal(t, "2.0.0", result.Packages[1].Version)
	})

	t.Run("get data for provider with no matches returns empty", func(t *testing.T) {
		testData := LocalPackageRoot{Packages: []LocalPackageItem{{SourceID: "pkg:npm/a", Version: "1"}}}
		jsonData, _ := json.Marshal(testData)
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return true },
			ReadFileFunc:                    func(path string) ([]byte, error) { return jsonData, nil },
		}
		parser := NewWithFileManager(mockFileManager)
		result := parser.GetDataForProvider("pypi")
		assert.Empty(t, result.Packages)
	})

	t.Run("get data for provider", func(t *testing.T) {
		testData := LocalPackageRoot{
			Packages: []LocalPackageItem{
				{SourceID: "pkg:npm/test-package", Version: "1.0.0"},
				{SourceID: "pkg:npm/another-package", Version: "2.0.0"},
				{SourceID: "pkg:pypi/black", Version: "3.0.0"},
			},
		}

		jsonData, _ := json.Marshal(testData)

		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string {
				return "/mock/path/local-packages.json"
			},
			FileExistsFunc: func(path string) bool {
				return true
			},
			ReadFileFunc: func(path string) ([]byte, error) {
				return jsonData, nil
			},
		}

		parser := NewWithFileManager(mockFileManager)
		result := parser.GetDataForProvider("npm")

		assert.Len(t, result.Packages, 2)
		// IDs are normalized to new format
		assert.Equal(t, "npm:test-package", result.Packages[0].SourceID)
		assert.Equal(t, "npm:another-package", result.Packages[1].SourceID)
	})

	t.Run("is package installed false when file exists but not present", func(t *testing.T) {
		testData := LocalPackageRoot{Packages: []LocalPackageItem{{SourceID: "pkg:npm/other", Version: "1"}}}
		jsonData, _ := json.Marshal(testData)
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return true },
			ReadFileFunc:                    func(path string) ([]byte, error) { return jsonData, nil },
		}
		parser := NewWithFileManager(mockFileManager)
		result := parser.IsPackageInstalled("pkg:npm/missing")
		assert.False(t, result)
	})

	t.Run("get data always reads from disk (force ignored)", func(t *testing.T) {
		readCount := 0
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return true },
			ReadFileFunc: func(path string) ([]byte, error) {
				readCount++
				return []byte(`{"packages":[]}`), nil
			},
		}
		parser := NewWithFileManager(mockFileManager)
		_ = parser.GetData(false)
		_ = parser.GetData(false)
		assert.Equal(t, 2, readCount)
	})

	t.Run("add local package new", func(t *testing.T) {
		var written []byte
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string {
				return "/mock/path/local-packages.json"
			},
			FileExistsFunc: func(path string) bool {
				return false
			},
			WriteFileFunc: func(path string, data []byte, perm uint32) error {
				written = data
				return nil
			},
		}

		parser := NewWithFileManager(mockFileManager)
		err := parser.AddLocalPackage("pkg:npm/new-package", "1.0.0")

		assert.NoError(t, err)
		var saved LocalPackageRoot
		_ = json.Unmarshal(written, &saved)
		assert.Len(t, saved.Packages, 1)
		// AddLocalPackage normalizes IDs to new format
		assert.Equal(t, "npm:new-package", saved.Packages[0].SourceID)
		assert.Equal(t, "1.0.0", saved.Packages[0].Version)
	})

	t.Run("add local package existing", func(t *testing.T) {
		existingData := LocalPackageRoot{
			Packages: []LocalPackageItem{
				{SourceID: "pkg:npm/existing-package", Version: "1.0.0"},
			},
		}

		jsonData, _ := json.Marshal(existingData)

		var written []byte
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string {
				return "/mock/path/local-packages.json"
			},
			FileExistsFunc: func(path string) bool {
				return true
			},
			ReadFileFunc: func(path string) ([]byte, error) {
				return jsonData, nil
			},
			WriteFileFunc: func(path string, data []byte, perm uint32) error {
				written = data
				return nil
			},
		}

		parser := NewWithFileManager(mockFileManager)
		err := parser.AddLocalPackage("pkg:npm/existing-package", "2.0.0")

		assert.NoError(t, err)
		var saved LocalPackageRoot
		_ = json.Unmarshal(written, &saved)
		assert.Len(t, saved.Packages, 1)
		// AddLocalPackage normalizes IDs to new format
		assert.Equal(t, "npm:existing-package", saved.Packages[0].SourceID)
		assert.Equal(t, "2.0.0", saved.Packages[0].Version)
	})

	t.Run("remove local package", func(t *testing.T) {
		existingData := LocalPackageRoot{
			Packages: []LocalPackageItem{
				{SourceID: "pkg:npm/package-to-remove", Version: "1.0.0"},
				{SourceID: "pkg:npm/keep-this", Version: "2.0.0"},
			},
		}

		jsonData, _ := json.Marshal(existingData)

		var written []byte
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string {
				return "/mock/path/local-packages.json"
			},
			FileExistsFunc: func(path string) bool {
				return true
			},
			ReadFileFunc: func(path string) ([]byte, error) {
				return jsonData, nil
			},
			WriteFileFunc: func(path string, data []byte, perm uint32) error {
				written = data
				return nil
			},
		}

		parser := NewWithFileManager(mockFileManager)
		err := parser.RemoveLocalPackage("pkg:npm/package-to-remove")

		assert.NoError(t, err)
		var saved LocalPackageRoot
		_ = json.Unmarshal(written, &saved)
		assert.Len(t, saved.Packages, 1)
		// IDs are normalized when reading from file, so pkg:npm/keep-this becomes npm:keep-this
		assert.Equal(t, "npm:keep-this", saved.Packages[0].SourceID)
	})

	t.Run("get by source id found", func(t *testing.T) {
		testData := LocalPackageRoot{
			Packages: []LocalPackageItem{
				{SourceID: "pkg:npm/test-package", Version: "1.0.0"},
				{SourceID: "pkg:pypi/black", Version: "2.0.0"},
			},
		}

		jsonData, _ := json.Marshal(testData)

		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string {
				return "/mock/path/local-packages.json"
			},
			FileExistsFunc: func(path string) bool {
				return true
			},
			ReadFileFunc: func(path string) ([]byte, error) {
				return jsonData, nil
			},
		}

		parser := NewWithFileManager(mockFileManager)
		result := parser.GetBySourceId("pkg:npm/test-package")

		// GetBySourceId normalizes IDs, and GetData also normalizes when reading from file
		assert.Equal(t, "npm:test-package", result.SourceID)
		assert.Equal(t, "1.0.0", result.Version)
	})

	t.Run("get by source id not found", func(t *testing.T) {
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string {
				return "/mock/path/local-packages.json"
			},
			FileExistsFunc: func(path string) bool {
				return false
			},
		}

		parser := NewWithFileManager(mockFileManager)
		result := parser.GetBySourceId("pkg:npm/nonexistent")

		assert.Equal(t, LocalPackageItem{}, result)
	})

	t.Run("is package installed true", func(t *testing.T) {
		testData := LocalPackageRoot{
			Packages: []LocalPackageItem{
				{SourceID: "pkg:npm/installed-package", Version: "1.0.0"},
			},
		}

		jsonData, _ := json.Marshal(testData)

		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string {
				return "/mock/path/local-packages.json"
			},
			FileExistsFunc: func(path string) bool {
				return true
			},
			ReadFileFunc: func(path string) ([]byte, error) {
				return jsonData, nil
			},
		}

		parser := NewWithFileManager(mockFileManager)
		result := parser.IsPackageInstalled("pkg:npm/installed-package")

		assert.True(t, result)
	})

	t.Run("is package installed false", func(t *testing.T) {
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string {
				return "/mock/path/local-packages.json"
			},
			FileExistsFunc: func(path string) bool {
				return false
			},
		}

		parser := NewWithFileManager(mockFileManager)
		result := parser.IsPackageInstalled("pkg:npm/nonexistent")

		assert.False(t, result)
	})

	t.Run("merge package integrations adds extras on item", func(t *testing.T) {
		// Start with one package in the file.
		existingData := LocalPackageRoot{
			Packages: []LocalPackageItem{
				{SourceID: "pkg:github/owner/repo", Version: "v1.0.0"},
			},
		}
		jsonData, _ := json.Marshal(existingData)

		var written []byte
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return true },
			ReadFileFunc:                    func(path string) ([]byte, error) { return jsonData, nil },
			WriteFileFunc:                   func(path string, data []byte, perm uint32) error { written = data; return nil },
		}

		parser := NewWithFileManager(mockFileManager)
		err := parser.MergePackageIntegrations("github:owner/repo", []string{"Neovim", "neovim", "  "})
		assert.NoError(t, err)

		var saved LocalPackageRoot
		_ = json.Unmarshal(written, &saved)
		assert.Len(t, saved.Packages, 1)
		assert.Equal(t, lockSchemaURL, saved.Schema)
		assert.Equal(t, "github:owner/repo", saved.Packages[0].SourceID) // normalized
		if assert.NotNil(t, saved.Packages[0].Extras) {
			assert.Equal(t, []string{"neovim"}, saved.Packages[0].Extras.Integrations)
		}
	})

	t.Run("merge package kind sets extras.kind", func(t *testing.T) {
		existingData := LocalPackageRoot{
			Packages: []LocalPackageItem{
				{SourceID: "github:folke/tokyonight.nvim", Version: "v1.0.0"},
			},
		}
		jsonData, _ := json.Marshal(existingData)

		var written []byte
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return true },
			ReadFileFunc:                    func(path string) ([]byte, error) { return jsonData, nil },
			WriteFileFunc:                   func(path string, data []byte, perm uint32) error { written = data; return nil },
		}

		parser := NewWithFileManager(mockFileManager)
		err := parser.MergePackageKind("github:folke/tokyonight.nvim", KindNeovimPlugin)
		assert.NoError(t, err)

		var saved LocalPackageRoot
		_ = json.Unmarshal(written, &saved)
		if assert.NotNil(t, saved.Packages[0].Extras) {
			assert.Equal(t, KindNeovimPlugin, saved.Packages[0].Extras.Kind)
		}
	})

	t.Run("merge package always_trust sets and clears", func(t *testing.T) {
		existingData := LocalPackageRoot{
			Packages: []LocalPackageItem{
				{SourceID: "npm:eslint", Version: "9.0.0"},
			},
		}
		jsonData, _ := json.Marshal(existingData)

		var written []byte
		mockFileManager := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return true },
			ReadFileFunc: func(path string) ([]byte, error) {
				if written != nil {
					return written, nil
				}
				return jsonData, nil
			},
			WriteFileFunc: func(path string, data []byte, perm uint32) error { written = data; return nil },
		}

		parser := NewWithFileManager(mockFileManager)
		err := parser.MergePackageAlwaysTrust("npm:eslint", true)
		assert.NoError(t, err)

		var saved LocalPackageRoot
		_ = json.Unmarshal(written, &saved)
		if assert.NotNil(t, saved.Packages[0].Extras) {
			assert.True(t, saved.Packages[0].Extras.AlwaysTrust)
		}

		err = parser.MergePackageAlwaysTrust("npm:eslint", false)
		assert.NoError(t, err)
		saved = LocalPackageRoot{}
		_ = json.Unmarshal(written, &saved)
		assert.Nil(t, saved.Packages[0].Extras)
	})
}

func TestMockFileManager(t *testing.T) {
	t.Run("mock file manager default behavior", func(t *testing.T) {
		mock := &MockFileManager{}

		path := mock.GetAppLocalPackagesFilePath()
		assert.Equal(t, "/mock/path/local-packages.json", path)

		exists := mock.FileExists("/test/path")
		assert.False(t, exists)

		data, err := mock.ReadFile("/test/path")
		assert.Nil(t, data)
		assert.Error(t, err)

		err = mock.WriteFile("/test/path", []byte("test"), 0644)
		assert.NoError(t, err)
	})

	t.Run("mock file manager custom behavior", func(t *testing.T) {
		mock := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string {
				return "/custom/path/packages.json"
			},
			FileExistsFunc: func(path string) bool {
				return path == "/custom/path/packages.json"
			},
			ReadFileFunc: func(path string) ([]byte, error) {
				return []byte(`{"packages":[]}`), nil
			},
			WriteFileFunc: func(path string, data []byte, perm uint32) error {
				return nil
			},
		}

		path := mock.GetAppLocalPackagesFilePath()
		assert.Equal(t, "/custom/path/packages.json", path)

		exists := mock.FileExists("/custom/path/packages.json")
		assert.True(t, exists)

		exists = mock.FileExists("/other/path")
		assert.False(t, exists)

		data, err := mock.ReadFile("/custom/path/packages.json")
		assert.NoError(t, err)
		assert.Equal(t, `{"packages":[]}`, string(data))

		err = mock.WriteFile("/custom/path/packages.json", []byte("test"), 0644)
		assert.NoError(t, err)
	})

	// Cover DefaultFileManager WriteFile method
	t.Run("default file manager write file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "lp.json")
		dfm := &DefaultFileManager{}
		data := []byte("hello")
		err := dfm.WriteFile(path, data, 0644)
		assert.NoError(t, err)
		read, err := os.ReadFile(path)
		assert.NoError(t, err)
		assert.Equal(t, data, read)
	})
}

func TestLegacyFunctions(t *testing.T) {
	t.Run("legacy functions still work", func(t *testing.T) {
		// Test that the legacy functions still work for backward compatibility
		// These will use the global parser instance
		assert.NotPanics(t, func() {
			_ = GetData(false)
			_ = GetDataForProvider("npm")
			_ = GetBySourceId("pkg:npm/test")
			_ = IsPackageInstalled("pkg:npm/test")
		})
	})

	// Ensure legacy AddLocalPackage and RemoveLocalPackage are covered without touching disk
	t.Run("legacy add and remove use global parser", func(t *testing.T) {
		var mem []byte
		mock := &MockFileManager{
			GetAppLocalPackagesFilePathFunc: func() string { return "/mock/path/local-packages.json" },
			FileExistsFunc:                  func(path string) bool { return len(mem) > 0 },
			ReadFileFunc:                    func(path string) ([]byte, error) { return mem, nil },
			WriteFileFunc:                   func(path string, data []byte, perm uint32) error { mem = data; return nil },
		}
		globalParser = NewWithFileManager(mock)

		err := AddLocalPackage("pkg:npm/legacy", "1.0.0")
		assert.NoError(t, err)
		var saved LocalPackageRoot
		_ = json.Unmarshal(mem, &saved)
		assert.Len(t, saved.Packages, 1)
		assert.Equal(t, lockSchemaURL, saved.Schema)
		// AddLocalPackage normalizes IDs to new format
		assert.Equal(t, "npm:legacy", saved.Packages[0].SourceID)
		assert.Equal(t, "1.0.0", saved.Packages[0].Version)

		err = RemoveLocalPackage("pkg:npm/legacy")
		assert.NoError(t, err)
		_ = json.Unmarshal(mem, &saved)
		assert.Len(t, saved.Packages, 0)
	})
}
