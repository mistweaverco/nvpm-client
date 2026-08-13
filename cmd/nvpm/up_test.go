package nvpm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/files"
	"github.com/mistweaverco/nvpm-client/internal/lib/local_packages_parser"
	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockRegistryProvider and MockUpdateChecker are defined in list_test.go
// They are available in this package for use in update tests

func TestUpdateAllPackagesGolden(t *testing.T) {
	// Update tests expect updates to proceed; disable min-release-age gating here.
	cfg.Flags.MinReleaseAge = 0

	oldDownload := downloadAndUnzipRegistryFn
	downloadAndUnzipRegistryFn = func() (bool, error) { return false, nil }
	t.Cleanup(func() { downloadAndUnzipRegistryFn = oldDownload })

	t.Run("update all packages with empty data", func(t *testing.T) {
		out := &MockOutputWriter{}
		prevFactory := newUpdateService
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(
				&MockLocalPackagesProvider{
					GetDataFunc: func(force bool) local_packages_parser.LocalPackageRoot {
						return local_packages_parser.LocalPackageRoot{Packages: []local_packages_parser.LocalPackageItem{}}
					},
				},
				&MockRegistryProvider{},
				&MockUpdateChecker{},
				out,
			)
		}
		defer func() { newUpdateService = prevFactory }()

		upCmd.Flags().Set("all", "true")
		upCmd.Run(upCmd, []string{})
		upCmd.Flags().Set("all", "false")

		assert.Contains(t, strings.Join(out.Output, "\n"), "No packages are currently installed")
	})

	t.Run("update all packages with all successful updates", func(t *testing.T) {
		// Set up mock provider factory for this test
		mockFactory := &providers.MockProviderFactory{
			MockNPMProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return true // All updates succeed
				},
			},
			MockPyPIProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return true // All updates succeed
				},
			},
		}
		providers.SetProviderFactory(mockFactory)
		defer providers.ResetProviderFactory()

		out := &MockOutputWriter{}
		prevUpdateService := newUpdateService
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(
				&MockLocalPackagesProvider{
					GetDataFunc: func(force bool) local_packages_parser.LocalPackageRoot {
						return local_packages_parser.LocalPackageRoot{
							Packages: []local_packages_parser.LocalPackageItem{
								{SourceID: "pkg:npm/test-package", Version: "1.0.0"},
								{SourceID: "pkg:pypi/black", Version: "2.0.0"},
							},
						}
					},
				},
				&MockRegistryProvider{
					GetLatestVersionFunc: func(sourceID string) string {
						// Return a newer version to indicate updates are available
						return "2.0.0"
					},
				},
				&MockUpdateChecker{
					CheckIfUpdateIsAvailableFunc: func(currentVersion, latestVersion string) (bool, string) {
						return true, "Update available"
					},
				},
				out,
			)
		}
		defer func() { newUpdateService = prevUpdateService }()

		upCmd.Flags().Set("all", "true")
		upCmd.Run(upCmd, []string{})
		upCmd.Flags().Set("all", "false")

		// Join all output and check for content
		allOutput := strings.Join(out.Output, "\n")
		assert.Contains(t, allOutput, "Found 2 installed packages")
		assert.Contains(t, allOutput, "[✓] Successfully updated pkg:npm/test-package")
		assert.Contains(t, allOutput, "[✓] Successfully updated pkg:pypi/black")
		assert.Contains(t, allOutput, "Successfully updated: 2")
		assert.Contains(t, allOutput, "Failed to update: 0")
	})

	t.Run("update all packages with mixed success", func(t *testing.T) {
		// Set up mock provider factory for this test
		mockFactory := &providers.MockProviderFactory{
			MockNPMProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return true // First package succeeds
				},
			},
			MockPyPIProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return false // Second package fails
				},
			},
		}
		providers.SetProviderFactory(mockFactory)
		defer providers.ResetProviderFactory()

		out := &MockOutputWriter{}
		prevUpdateService := newUpdateService
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(
				&MockLocalPackagesProvider{
					GetDataFunc: func(force bool) local_packages_parser.LocalPackageRoot {
						return local_packages_parser.LocalPackageRoot{
							Packages: []local_packages_parser.LocalPackageItem{
								{SourceID: "pkg:npm/success-package", Version: "1.0.0"},
								{SourceID: "pkg:pypi/failed-package", Version: "2.0.0"},
							},
						}
					},
				},
				&MockRegistryProvider{
					GetLatestVersionFunc: func(sourceID string) string {
						return "2.0.0"
					},
				},
				&MockUpdateChecker{
					CheckIfUpdateIsAvailableFunc: func(currentVersion, latestVersion string) (bool, string) {
						return true, "Update available"
					},
				},
				out,
			)
		}
		defer func() { newUpdateService = prevUpdateService }()

		upCmd.Flags().Set("all", "true")
		upCmd.Run(upCmd, []string{})
		upCmd.Flags().Set("all", "false")

		// Join all output and check for content
		allOutput := strings.Join(out.Output, "\n")
		assert.Contains(t, allOutput, "Found 2 installed packages")
		assert.Contains(t, allOutput, "[✓] Successfully updated pkg:npm/success-package")
		assert.Contains(t, allOutput, "[✗] Failed to update pkg:pypi/failed-package")
		assert.Contains(t, allOutput, "Successfully updated: 1")
		assert.Contains(t, allOutput, "Failed to update: 1")
	})

	t.Run("update all packages with all failures", func(t *testing.T) {
		// Set up mock provider factory for this test
		mockFactory := &providers.MockProviderFactory{
			MockNPMProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return false // All updates fail
				},
			},
			MockPyPIProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return false // All updates fail
				},
			},
		}
		providers.SetProviderFactory(mockFactory)
		defer providers.ResetProviderFactory()

		out := &MockOutputWriter{}
		prevUpdateService := newUpdateService
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(
				&MockLocalPackagesProvider{
					GetDataFunc: func(force bool) local_packages_parser.LocalPackageRoot {
						return local_packages_parser.LocalPackageRoot{
							Packages: []local_packages_parser.LocalPackageItem{
								{SourceID: "pkg:npm/eslint", Version: "1.0.0"},
								{SourceID: "pkg:pypi/black", Version: "2.0.0"},
							},
						}
					},
				},
				&MockRegistryProvider{
					GetLatestVersionFunc: func(sourceID string) string {
						return "2.0.0"
					},
				},
				&MockUpdateChecker{
					CheckIfUpdateIsAvailableFunc: func(currentVersion, latestVersion string) (bool, string) {
						return true, "Update available"
					},
				},
				out,
			)
		}
		defer func() { newUpdateService = prevUpdateService }()

		upCmd.Flags().Set("all", "true")
		upCmd.Run(upCmd, []string{})
		upCmd.Flags().Set("all", "false")

		allOutput := strings.Join(out.Output, "\n")
		assert.Contains(t, allOutput, "Found 2 installed packages")
		assert.Contains(t, allOutput, "[✗] Failed to update pkg:npm/eslint")
		assert.Contains(t, allOutput, "[✗] Failed to update pkg:pypi/black")
		assert.Contains(t, allOutput, "Successfully updated: 0")
		assert.Contains(t, allOutput, "Failed to update: 2")
	})
}

func TestUpdateCommand(t *testing.T) {
	t.Run("up command structure", func(t *testing.T) {
		assert.Equal(t, "up", upCmd.Use)
		assert.Equal(t, "Update packages to their latest versions", upCmd.Short)
		assert.NotEmpty(t, upCmd.Long)
		assert.Contains(t, upCmd.Aliases, "update")
	})

	t.Run("update command has all flag", func(t *testing.T) {
		allFlag := upCmd.Flags().Lookup("all")
		assert.NotNil(t, allFlag)
		assert.Equal(t, "all", allFlag.Name)
		assert.Equal(t, "A", allFlag.Shorthand)
		assert.Equal(t, "Update all installed packages to their latest versions", allFlag.Usage)
	})

	t.Run("update command has always-trust and filter flags", func(t *testing.T) {
		assert.NotNil(t, upCmd.Flags().Lookup("always-trust"))
		assert.NotNil(t, upCmd.Flags().Lookup("no-always-trust"))
		assert.NotNil(t, upCmd.Flags().Lookup("filter"))
	})
}

func TestUpdateCommandRunPaths(t *testing.T) {
	t.Run("prints error when no args and no --all", func(t *testing.T) {
		captured := &MockOutputWriter{}
		prevFactory := newUpdateService
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(&MockLocalPackagesProvider{}, &MockRegistryProvider{}, &MockUpdateChecker{}, captured)
		}
		defer func() { newUpdateService = prevFactory }()

		upCmd.SetArgs([]string{})
		upCmd.Flags().Set("all", "false")
		upCmd.Run(upCmd, []string{})

		all := strings.Join(captured.Output, "\n")
		assert.Contains(t, all, "Please provide package IDs or use --all flag")
	})

	t.Run("validates pkg id and provider", func(t *testing.T) {
		// Invalid prefix
		out1 := &MockOutputWriter{}
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(&MockLocalPackagesProvider{}, &MockRegistryProvider{}, &MockUpdateChecker{}, out1)
		}
		upCmd.Run(upCmd, []string{"invalid:id"})
		assert.Contains(t, strings.Join(out1.Output, "\n"), "Unsupported provider")

		// Missing provider/package
		out2 := &MockOutputWriter{}
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(&MockLocalPackagesProvider{}, &MockRegistryProvider{}, &MockUpdateChecker{}, out2)
		}
		upCmd.Run(upCmd, []string{"pkg:only"})
		assert.Contains(t, strings.Join(out2.Output, "\n"), "invalid package ID format")

		// Unsupported provider
		out3 := &MockOutputWriter{}
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(&MockLocalPackagesProvider{}, &MockRegistryProvider{}, &MockUpdateChecker{}, out3)
		}
		upCmd.Run(upCmd, []string{"pkg:unknown/pkg"})
		assert.Contains(t, strings.Join(out3.Output, "\n"), "Unsupported provider")
	})

	t.Run("updates when valid id", func(t *testing.T) {
		// Set up mock provider factory for this test
		mockFactory := &providers.MockProviderFactory{
			MockNPMProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return true // Update succeeds
				},
			},
		}
		providers.SetProviderFactory(mockFactory)
		defer providers.ResetProviderFactory()

		out := &MockOutputWriter{}
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(&MockLocalPackagesProvider{}, &MockRegistryProvider{}, &MockUpdateChecker{}, out)
		}
		upCmd.Run(upCmd, []string{"pkg:npm/eslint"})
		assert.Contains(t, strings.Join(out.Output, "\n"), "[✓] Successfully updated npm:eslint")
	})

	t.Run("updates multiple packages successfully", func(t *testing.T) {
		// Set up mock provider factory for this test
		mockFactory := &providers.MockProviderFactory{
			MockNPMProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return true // All updates succeed
				},
			},
			MockPyPIProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return true // All updates succeed
				},
			},
		}
		providers.SetProviderFactory(mockFactory)
		defer providers.ResetProviderFactory()

		out := &MockOutputWriter{}
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(&MockLocalPackagesProvider{}, &MockRegistryProvider{}, &MockUpdateChecker{}, out)
		}
		upCmd.Run(upCmd, []string{"pkg:npm/eslint", "pkg:pypi/black"})
		allOutput := strings.Join(out.Output, "\n")
		assert.Contains(t, allOutput, "[✓] Successfully updated npm:eslint")
		assert.Contains(t, allOutput, "[✓] Successfully updated pypi:black")
		assert.Contains(t, allOutput, "Successfully updated: 2")
		assert.Contains(t, allOutput, "Failed to update: 0")
	})

	t.Run("--all path calls UpdateAllPackages", func(t *testing.T) {
		out := &MockOutputWriter{}
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(
				&MockLocalPackagesProvider{GetDataFunc: func(force bool) local_packages_parser.LocalPackageRoot {
					return local_packages_parser.LocalPackageRoot{Packages: []local_packages_parser.LocalPackageItem{}}
				}},
				&MockRegistryProvider{},
				&MockUpdateChecker{},
				out,
			)
		}
		upCmd.Flags().Set("all", "true")
		upCmd.Run(upCmd, []string{})
		upCmd.Flags().Set("all", "false")
		assert.Contains(t, strings.Join(out.Output, "\n"), "Updating all installed packages to latest versions...")
	})
}

func TestMockOutputWriter(t *testing.T) {
	t.Run("mock output writer default behavior", func(t *testing.T) {
		mock := &MockOutputWriter{}

		mock.Println("test")
		mock.Printf("format %s", "test")

		assert.Len(t, mock.Output, 2)
		assert.Contains(t, mock.Output, "test")
		assert.Contains(t, mock.Output, "format test")
	})

	t.Run("mock output writer custom behavior", func(t *testing.T) {
		captured := []string{}

		mock := &MockOutputWriter{
			PrintlnFunc: func(args ...interface{}) {
				captured = append(captured, "custom println")
			},
			PrintfFunc: func(format string, args ...interface{}) {
				captured = append(captured, "custom printf")
			},
		}

		mock.Println("test")
		mock.Printf("format %s", "test")

		assert.Len(t, captured, 2)
		assert.Contains(t, captured, "custom println")
		assert.Contains(t, captured, "custom printf")
	})
}

func TestUpdateCommandFullOutputGolden(t *testing.T) {
	t.Run("update all with mixed success/failure and full summary", func(t *testing.T) {
		// Set up mock provider factory for this test
		mockFactory := &providers.MockProviderFactory{
			MockNPMProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return true // First package succeeds
				},
			},
			MockPyPIProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return false // Second package fails
				},
			},
			MockGolangProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return true // Third package succeeds
				},
			},
		}
		providers.SetProviderFactory(mockFactory)
		defer providers.ResetProviderFactory()

		out := &MockOutputWriter{}
		prevUpdateService := newUpdateService
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(
				&MockLocalPackagesProvider{
					GetDataFunc: func(force bool) local_packages_parser.LocalPackageRoot {
						return local_packages_parser.LocalPackageRoot{
							Packages: []local_packages_parser.LocalPackageItem{
								{SourceID: "pkg:npm/eslint", Version: "1.0.0"},
								{SourceID: "pkg:pypi/black", Version: "2.0.0"},
								{SourceID: "pkg:golang/gopls", Version: "0.1.0"},
							},
						}
					},
				},
				&MockRegistryProvider{
					GetLatestVersionFunc: func(sourceID string) string {
						return "2.0.0"
					},
				},
				&MockUpdateChecker{
					CheckIfUpdateIsAvailableFunc: func(currentVersion, latestVersion string) (bool, string) {
						return true, "Update available"
					},
				},
				out,
			)
		}
		defer func() { newUpdateService = prevUpdateService }()

		upCmd.Flags().Set("all", "true")
		upCmd.Run(upCmd, []string{})
		upCmd.Flags().Set("all", "false")

		allOutput := strings.Join(out.Output, "\n")
		assert.Contains(t, allOutput, "Found 3 installed packages")
		assert.Contains(t, allOutput, "[✓] Successfully updated pkg:npm/eslint")
		assert.Contains(t, allOutput, "[✗] Failed to update pkg:pypi/black")
		assert.Contains(t, allOutput, "[✓] Successfully updated pkg:golang/gopls")
		assert.Contains(t, allOutput, "Successfully updated: 2")
		assert.Contains(t, allOutput, "Failed to update: 1")
	})

	t.Run("update all with all failures", func(t *testing.T) {
		// Set up mock provider factory for this test
		mockFactory := &providers.MockProviderFactory{
			MockNPMProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return false // All updates fail
				},
			},
			MockPyPIProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return false // All updates fail
				},
			},
		}
		providers.SetProviderFactory(mockFactory)
		defer providers.ResetProviderFactory()

		out := &MockOutputWriter{}
		prevUpdateService := newUpdateService
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(
				&MockLocalPackagesProvider{
					GetDataFunc: func(force bool) local_packages_parser.LocalPackageRoot {
						return local_packages_parser.LocalPackageRoot{
							Packages: []local_packages_parser.LocalPackageItem{
								{SourceID: "pkg:npm/eslint", Version: "1.0.0"},
								{SourceID: "pkg:pypi/black", Version: "2.0.0"},
							},
						}
					},
				},
				&MockRegistryProvider{
					GetLatestVersionFunc: func(sourceID string) string {
						return "2.0.0"
					},
				},
				&MockUpdateChecker{
					CheckIfUpdateIsAvailableFunc: func(currentVersion, latestVersion string) (bool, string) {
						return true, "Update available"
					},
				},
				out,
			)
		}
		defer func() { newUpdateService = prevUpdateService }()

		upCmd.Flags().Set("all", "true")
		upCmd.Run(upCmd, []string{})
		upCmd.Flags().Set("all", "false")

		allOutput := strings.Join(out.Output, "\n")
		assert.Contains(t, allOutput, "Found 2 installed packages")
		assert.Contains(t, allOutput, "[✗] Failed to update pkg:npm/eslint")
		assert.Contains(t, allOutput, "[✗] Failed to update pkg:pypi/black")
		assert.Contains(t, allOutput, "Successfully updated: 0")
		assert.Contains(t, allOutput, "Failed to update: 2")
	})

	t.Run("update all with all successes", func(t *testing.T) {
		// Set up mock provider factory for this test
		mockFactory := &providers.MockProviderFactory{
			MockNPMProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return true // All updates succeed
				},
			},
		}
		providers.SetProviderFactory(mockFactory)
		defer providers.ResetProviderFactory()

		out := &MockOutputWriter{}
		prevUpdateService := newUpdateService
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(
				&MockLocalPackagesProvider{
					GetDataFunc: func(force bool) local_packages_parser.LocalPackageRoot {
						return local_packages_parser.LocalPackageRoot{
							Packages: []local_packages_parser.LocalPackageItem{
								{SourceID: "pkg:npm/eslint", Version: "1.0.0"},
							},
						}
					},
				},
				&MockRegistryProvider{
					GetLatestVersionFunc: func(sourceID string) string {
						return "2.0.0"
					},
				},
				&MockUpdateChecker{
					CheckIfUpdateIsAvailableFunc: func(currentVersion, latestVersion string) (bool, string) {
						return true, "Update available"
					},
				},
				out,
			)
		}
		defer func() { newUpdateService = prevUpdateService }()

		upCmd.Flags().Set("all", "true")
		upCmd.Run(upCmd, []string{})
		upCmd.Flags().Set("all", "false")

		allOutput := strings.Join(out.Output, "\n")
		assert.Contains(t, allOutput, "Found 1 installed packages")
		assert.Contains(t, allOutput, "[✓] Successfully updated pkg:npm/eslint")
		assert.Contains(t, allOutput, "Successfully updated: 1")
		assert.Contains(t, allOutput, "Failed to update: 0")
	})

	t.Run("update single package failure", func(t *testing.T) {
		// Set up mock provider factory for this test
		mockFactory := &providers.MockProviderFactory{
			MockNPMProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return false // Update fails
				},
			},
		}
		providers.SetProviderFactory(mockFactory)
		defer providers.ResetProviderFactory()

		out := &MockOutputWriter{}
		prevUpdateService := newUpdateService
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(&MockLocalPackagesProvider{}, &MockRegistryProvider{}, &MockUpdateChecker{}, out)
		}
		defer func() { newUpdateService = prevUpdateService }()

		upCmd.Run(upCmd, []string{"pkg:npm/eslint"})

		allOutput := strings.Join(out.Output, "\n")
		assert.Contains(t, allOutput, "[✗] Failed to update npm:eslint")
	})

	t.Run("update single package success", func(t *testing.T) {
		// Set up mock provider factory for this test
		mockFactory := &providers.MockProviderFactory{
			MockNPMProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return true // Update succeeds
				},
			},
		}
		providers.SetProviderFactory(mockFactory)
		defer providers.ResetProviderFactory()

		out := &MockOutputWriter{}
		prevUpdateService := newUpdateService
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(&MockLocalPackagesProvider{}, &MockRegistryProvider{}, &MockUpdateChecker{}, out)
		}
		defer func() { newUpdateService = prevUpdateService }()

		upCmd.Run(upCmd, []string{"pkg:npm/eslint"})

		allOutput := strings.Join(out.Output, "\n")
		assert.Contains(t, allOutput, "[✓] Successfully updated npm:eslint")
	})

	t.Run("update multiple packages with mixed results", func(t *testing.T) {
		// Set up mock provider factory for this test
		mockFactory := &providers.MockProviderFactory{
			MockNPMProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return true // First package succeeds
				},
			},
			MockPyPIProvider: &providers.MockPackageManager{
				UpdateFunc: func(sourceID string) bool {
					return false // Second package fails
				},
			},
		}
		providers.SetProviderFactory(mockFactory)
		defer providers.ResetProviderFactory()

		out := &MockOutputWriter{}
		prevUpdateService := newUpdateService
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(&MockLocalPackagesProvider{}, &MockRegistryProvider{}, &MockUpdateChecker{}, out)
		}
		defer func() { newUpdateService = prevUpdateService }()

		upCmd.Run(upCmd, []string{"pkg:npm/eslint", "pkg:pypi/black"})

		allOutput := strings.Join(out.Output, "\n")
		assert.Contains(t, allOutput, "[✓] Successfully updated npm:eslint")
		assert.Contains(t, allOutput, "[✗] Failed to update pypi:black")
		assert.Contains(t, allOutput, "Some packages failed to update.")
	})

	t.Run("skips pinned versions instead of falsely succeeding", func(t *testing.T) {
		out := &MockOutputWriter{}
		prevUpdateService := newUpdateService
		newUpdateService = func() *UpdateService {
			return NewUpdateServiceWithDependencies(
				&MockLocalPackagesProvider{},
				&MockRegistryProvider{},
				&MockUpdateChecker{},
				out,
			)
		}
		defer func() { newUpdateService = prevUpdateService }()

		upCmd.Run(upCmd, []string{"kulala.nvim@v6.0.0", "github:mistweaverco/kulala.nvim@v6.0.0"})

		allOutput := strings.Join(out.Output, "\n")
		assert.Contains(t, allOutput, "cannot target a specific version")
		assert.Contains(t, allOutput, "nvpm add kulala.nvim@v6.0.0")
		assert.Contains(t, allOutput, "nvpm set")
		assert.Contains(t, allOutput, "No packages updated.")
		assert.NotContains(t, allOutput, "Successfully updated")
	})
}

func TestUpdateAllPackagesDoesNotInstallExtrasWhenUpToDate(t *testing.T) {
	sourceID := setupUpExtraPackagesFixture(t, "2.5.0")
	out := &MockOutputWriter{}
	svc := NewUpdateServiceWithDependencies(
		&defaultLocalPackagesProvider{},
		&MockRegistryProvider{
			GetLatestVersionFunc:  func(string) string { return "2.5.0" },
			GetLatestVersionsFunc: func(string) (string, string) { return "2.5.0", "" },
		},
		&MockUpdateChecker{
			CheckIfUpdateIsAvailableFunc: func(string, string) (bool, string) { return false, "" },
		},
		out,
	)
	assert.True(t, svc.UpdateAllPackages())
	lock := local_packages_parser.GetBySourceId(sourceID)
	if lock.Extras != nil {
		assert.Empty(t, lock.Extras.ExtraPackages)
	}
	assert.NotContains(t, strings.Join(out.Output, "\n"), "Extra packages installed for "+sourceID)
}

func TestUpdateAllPackagesRecordsExtraPackagesOnUpdate(t *testing.T) {
	sourceID := setupUpExtraPackagesFixture(t, "2.4.0")
	mockFactory := &providers.MockProviderFactory{
		MockNPMProvider: &providers.MockPackageManager{
			UpdateFunc: func(id string) bool {
				_ = local_packages_parser.AddLocalPackage(id, "2.5.0")
				return true
			},
		},
	}
	providers.SetProviderFactory(mockFactory)
	t.Cleanup(providers.ResetProviderFactory)

	out := &MockOutputWriter{}
	svc := NewUpdateServiceWithDependencies(
		&defaultLocalPackagesProvider{},
		&MockRegistryProvider{
			GetLatestVersionFunc:  func(string) string { return "2.5.0" },
			GetLatestVersionsFunc: func(string) (string, string) { return "2.5.0", "" },
		},
		&MockUpdateChecker{
			CheckIfUpdateIsAvailableFunc: func(string, string) (bool, string) { return true, "Update available" },
		},
		out,
	)
	assert.True(t, svc.UpdateAllPackages())
	lock := local_packages_parser.GetBySourceId(sourceID)
	require.NotNil(t, lock.Extras)
	assert.Equal(t, []local_packages_parser.ExtraPackagePin{
		{ID: "npm:@astrojs/ts-plugin", Version: "1.2.3"},
		{ID: "npm:typescript", Version: "6.0.3"},
	}, lock.Extras.ExtraPackages)
	assert.Contains(t, strings.Join(out.Output, "\n"), "Extra packages installed for "+sourceID)
}

func TestUpdateAllPackagesDoesNotInstallExtrasWhenUpdateSkipped(t *testing.T) {
	sourceID := setupUpExtraPackagesFixture(t, "2.4.0")
	mockFactory := &providers.MockProviderFactory{
		MockNPMProvider: &providers.MockPackageManager{
			UpdateFunc: func(string) bool {
				providers.SetLastSkip("waiting for min-release-age")
				return false
			},
		},
	}
	providers.SetProviderFactory(mockFactory)
	t.Cleanup(providers.ResetProviderFactory)

	out := &MockOutputWriter{}
	svc := NewUpdateServiceWithDependencies(
		&defaultLocalPackagesProvider{},
		&MockRegistryProvider{
			GetLatestVersionFunc:  func(string) string { return "2.5.0" },
			GetLatestVersionsFunc: func(string) (string, string) { return "2.5.0", "" },
		},
		&MockUpdateChecker{
			CheckIfUpdateIsAvailableFunc: func(string, string) (bool, string) { return true, "Update available" },
		},
		out,
	)
	assert.True(t, svc.UpdateAllPackages())
	lock := local_packages_parser.GetBySourceId(sourceID)
	assert.Equal(t, "2.4.0", lock.Version)
	if lock.Extras != nil {
		assert.Empty(t, lock.Extras.ExtraPackages)
	}
	allOutput := strings.Join(out.Output, "\n")
	assert.Contains(t, allOutput, "Skipped "+sourceID)
	assert.NotContains(t, allOutput, "Extra packages installed for "+sourceID)
}

func TestUpdateSinglePackageDoesNotInstallExtrasWhenUpToDate(t *testing.T) {
	sourceID := setupUpExtraPackagesFixture(t, "2.5.0")
	out := &MockOutputWriter{}
	prev := newUpdateService
	newUpdateService = func() *UpdateService {
		return NewUpdateServiceWithDependencies(
			&defaultLocalPackagesProvider{},
			&MockRegistryProvider{
				GetLatestVersionFunc:  func(string) string { return "2.5.0" },
				GetLatestVersionsFunc: func(string) (string, string) { return "2.5.0", "" },
			},
			&MockUpdateChecker{
				CheckIfUpdateIsAvailableFunc: func(string, string) (bool, string) { return false, "" },
			},
			out,
		)
	}
	t.Cleanup(func() { newUpdateService = prev })
	_ = upCmd.Flags().Set("all", "false")

	upCmd.Run(upCmd, []string{sourceID})
	lock := local_packages_parser.GetBySourceId(sourceID)
	if lock.Extras != nil {
		assert.Empty(t, lock.Extras.ExtraPackages)
	}
	assert.NotContains(t, strings.Join(out.Output, "\n"), "Extra packages installed for "+sourceID)
}

func setupUpExtraPackagesFixture(t *testing.T, installedVersion string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("NVPM_HOME", t.TempDir())
	t.Setenv("NVPM_CACHE", t.TempDir())
	cfg.Flags.MinReleaseAge = 0
	oldDownload := downloadAndUnzipRegistryFn
	downloadAndUnzipRegistryFn = func() (bool, error) { return false, nil }
	t.Cleanup(func() { downloadAndUnzipRegistryFn = oldDownload })

	sourceID := "npm:@astrojs/language-server"
	require.NoError(t, local_packages_parser.AddLocalPackage(sourceID, installedVersion))

	items := []registry_parser.RegistryItem{{
		Name:    "astro-language-server",
		Version: "2.5.0",
		Source: registry_parser.RegistryItemSource{
			ID:            sourceID,
			ExtraPackages: []string{"npm:typescript@6.0.3", "npm:@astrojs/ts-plugin"},
		},
	}}
	data, err := json.Marshal(items)
	require.NoError(t, err)
	regPath := files.GetAppRegistryFilePath()
	require.NoError(t, os.MkdirAll(filepath.Dir(regPath), 0755))
	require.NoError(t, os.WriteFile(regPath, data, 0644))

	restore := providers.StubExtraPackageInstallForTest(
		func(string, []string, string, []string) (int, error) { return 0, nil },
		func(provider, name string) (string, error) {
			if name == "@astrojs/ts-plugin" {
				return "1.2.3", nil
			}
			return "", nil
		},
	)
	t.Cleanup(restore)
	return sourceID
}
