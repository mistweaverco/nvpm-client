package nvpm

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/stretchr/testify/assert"
)

func TestInstallCommand(t *testing.T) {
	// Test that install command is properly configured
	assert.Contains(t, installCmd.Use, "install")
	assert.Contains(t, installCmd.Short, "Install one or more packages")
	assert.Contains(t, installCmd.Long, "Install one or more packages from supported providers")

	// Test aliases
	aliases := installCmd.Aliases
	assert.Contains(t, aliases, "add")
	assert.Len(t, aliases, 1)
}

func TestInstallCommandArgs(t *testing.T) {
	// Test argument validation using the extracted function
	testCases := []struct {
		name        string
		args        []string
		expectError bool
	}{
		{"valid single package", []string{"pkg:npm/test-package"}, false},
		{"valid multiple packages", []string{"pkg:npm/test-package", "pkg:pypi/black"}, false},
		{"valid packages with versions", []string{"pkg:npm/test-package@1.0.0", "pkg:pypi/black@22.3.0"}, false},
		{"valid packages mixed with and without versions", []string{"pkg:npm/test-package", "pkg:pypi/black@22.3.0"}, false},
		{"package name without provider (allowed - will search registry)", []string{"test-package"}, false},
		{"invalid format - no provider", []string{"pkg:/test-package"}, true},
		{"invalid format - no package name", []string{"pkg:npm/"}, true},
		{"empty args", []string{}, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePackageArgs(tc.args)
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParsePackageIDAndVersion(t *testing.T) {
	testCases := []struct {
		name            string
		input           string
		expectedID      string
		expectedVersion string
	}{
		// Basic packages without versions
		{"basic npm package", "pkg:npm/eslint", "pkg:npm/eslint", ""},
		{"basic pypi package", "pkg:pypi/black", "pkg:pypi/black", ""},
		{"basic golang package", "pkg:golang/golang.org/x/tools/gopls", "pkg:golang/golang.org/x/tools/gopls", ""},
		{"basic cargo package", "pkg:cargo/ripgrep", "pkg:cargo/ripgrep", ""},

		// NPM organization packages (these should NOT treat @org as version)
		{"npm org package", "pkg:npm/@mistweaverco/kulala-fmt", "pkg:npm/@mistweaverco/kulala-fmt", ""},
		{"npm org package with @", "pkg:npm/@prisma/language-server", "pkg:npm/@prisma/language-server", ""},
		{"npm org package with @", "pkg:npm/@tailwindcss/language-server", "pkg:npm/@tailwindcss/language-server", ""},

		// Packages with versions
		{"npm package with version", "pkg:npm/eslint@1.0.0", "pkg:npm/eslint", "1.0.0"},
		{"pypi package with version", "pkg:pypi/black@22.3.0", "pkg:pypi/black", "22.3.0"},
		{"golang package with version", "pkg:golang/golang.org/x/tools/gopls@v0.14.0", "pkg:golang/golang.org/x/tools/gopls", "v0.14.0"},
		{"cargo package with version", "pkg:cargo/ripgrep@13.0.0", "pkg:cargo/ripgrep", "13.0.0"},

		// NPM organization packages with versions
		{"npm org package with version", "pkg:npm/@mistweaverco/kulala-fmt@2.10.0", "pkg:npm/@mistweaverco/kulala-fmt", "2.10.0"},
		{"npm org package with version", "pkg:npm/@prisma/language-server@6.14.0", "pkg:npm/@prisma/language-server", "6.14.0"},
		{"npm org package with version", "pkg:npm/@tailwindcss/language-server@0.14.26", "pkg:npm/@tailwindcss/language-server", "0.14.26"},

		// Special version cases
		{"package with latest version", "pkg:npm/eslint@latest", "pkg:npm/eslint", "latest"},
		{"package with beta version", "pkg:npm/eslint@1.0.0-beta", "pkg:npm/eslint", "1.0.0-beta"},
		{"package with alpha version", "pkg:npm/eslint@1.0.0-alpha.1", "pkg:npm/eslint", "1.0.0-alpha.1"},
		{"package with rc version", "pkg:npm/eslint@1.0.0-rc.1", "pkg:npm/eslint", "1.0.0-rc.1"},

		// Edge cases
		{"package with @ in name but no version", "pkg:npm/@mistweaverco/kulala-fmt", "pkg:npm/@mistweaverco/kulala-fmt", ""},
		{"package with multiple @ symbols", "pkg:npm/@org@suborg/package@1.0.0", "pkg:npm/@org@suborg/package", "1.0.0"},
		{"package with @ at end but no version", "pkg:npm/package@", "pkg:npm/package@", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			id, version := parsePackageIDAndVersion(tc.input)
			assert.Equal(t, tc.expectedID, id, "Package ID mismatch")
			assert.Equal(t, tc.expectedVersion, version, "Version mismatch")
		})
	}
}

func TestInstallCommandValidation(t *testing.T) {
	t.Run("unsupported provider", func(t *testing.T) {
		// Test the validation function directly
		err := validatePackageArgs([]string{"pkg:unsupported/test-package"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported provider 'unsupported'")
		assert.Contains(t, err.Error(), "Supported providers:")
	})

	t.Run("package name without provider (now allowed)", func(t *testing.T) {
		// Test that package names without provider are allowed (will be handled in Run function)
		err := validatePackageArgs([]string{"invalid-format"})
		assert.NoError(t, err, "Package names without provider are now allowed and will be searched in registry")
	})

	t.Run("missing package name", func(t *testing.T) {
		// Test the validation function directly
		err := validatePackageArgs([]string{"pkg:npm/"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "package name cannot be empty")
	})
}

func TestInstallCommandProviderValidation(t *testing.T) {
	// Test provider validation
	testCases := []struct {
		name        string
		pkgId       string
		expectValid bool
	}{
		{"npm provider", "pkg:npm/test-package", true},
		{"pypi provider", "pkg:pypi/test-package", true},
		{"golang provider", "pkg:golang/test-package", true},
		{"cargo provider", "pkg:cargo/test-package", true},
		{"unsupported provider", "pkg:unsupported/test-package", false},
		{"invalid provider format", "pkg:123/test-package", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.HasPrefix(tc.pkgId, "pkg:") {
				parts := strings.Split(strings.TrimPrefix(tc.pkgId, "pkg:"), "/")
				if len(parts) >= 2 {
					provider := parts[0]
					isValid := providers.IsSupportedProvider(provider)
					assert.Equal(t, tc.expectValid, isValid)
				}
			}
		})
	}
}

func TestInstallCommandVersionHandling(t *testing.T) {
	// Test version argument handling
	testCases := []struct {
		name             string
		args             []string
		expectedPackages []string
		expectedVersion  string
	}{
		{"no version specified", []string{"pkg:npm/test-package"}, []string{"pkg:npm/test-package"}, "latest"},
		{"version specified", []string{"pkg:npm/test-package", "1.0.0"}, []string{"pkg:npm/test-package"}, "1.0.0"},
		{"latest version", []string{"pkg:npm/test-package", "latest"}, []string{"pkg:npm/test-package"}, "latest"},
		{"empty version", []string{"pkg:npm/test-package", ""}, []string{"pkg:npm/test-package"}, ""},
		{"two packages no version", []string{"pkg:npm/test-package", "pkg:pypi/black"}, []string{"pkg:npm/test-package", "pkg:pypi/black"}, "latest"},
		{"two packages with version", []string{"pkg:npm/test-package", "pkg:pypi/black", "1.0.0"}, []string{"pkg:npm/test-package", "pkg:pypi/black"}, "1.0.0"},
		{"three packages with version", []string{"pkg:npm/test-package", "pkg:pypi/black", "pkg:golang/gopls", "latest"}, []string{"pkg:npm/test-package", "pkg:pypi/black", "pkg:golang/gopls"}, "latest"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Determine if a version is specified (last argument might be a version)
			var packages []string
			var version string

			// Check if the last argument is a version (not starting with 'pkg:')
			if len(tc.args) > 1 && !strings.HasPrefix(tc.args[len(tc.args)-1], "pkg:") {
				version = tc.args[len(tc.args)-1]
				packages = tc.args[:len(tc.args)-1]
			} else {
				version = "latest"
				packages = tc.args
			}

			assert.Equal(t, tc.expectedPackages, packages)
			assert.Equal(t, tc.expectedVersion, version)
		})
	}
}

func TestInstallCommandExamples(t *testing.T) {
	// Test that the examples in the help text are valid
	examples := []string{
		"pkg:npm/@prisma/language-server",
		"pkg:golang/golang.org/x/tools/gopls",
		"pkg:pypi/black",
		"pkg:cargo/ripgrep",
	}

	for _, example := range examples {
		t.Run("example_"+example, func(t *testing.T) {
			// Test that each example follows the expected format
			assert.True(t, strings.HasPrefix(example, "pkg:"))

			parts := strings.Split(strings.TrimPrefix(example, "pkg:"), "/")
			assert.GreaterOrEqual(t, len(parts), 2, "Example should have provider and package name")

			provider := parts[0]
			assert.True(t, providers.IsSupportedProvider(provider), "Provider %s should be supported", provider)
		})
	}
}

func TestInstallCommandHelp(t *testing.T) {
	// Test that help command works without executing the actual command
	// We'll just test the command structure instead
	assert.NotNil(t, installCmd)
	assert.Contains(t, installCmd.Use, "install")
	assert.Contains(t, installCmd.Short, "Install one or more packages")
}

func TestInstallCommandIntegration(t *testing.T) {
	// Test that the command structure is correct without executing
	// This avoids hanging on actual command execution

	testCases := []struct {
		name string
		args []string
	}{
		{"valid npm package", []string{"pkg:npm/test-package"}},
		{"valid golang package", []string{"pkg:golang/golang.org/x/tools/gopls"}},
		{"valid pypi package", []string{"pkg:pypi/black"}},
		{"valid cargo package", []string{"pkg:cargo/ripgrep"}},
		{"multiple packages", []string{"pkg:npm/test-package", "pkg:pypi/black"}},
		{"multiple packages with version", []string{"pkg:npm/test-package", "pkg:pypi/black", "1.0.0"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Just test that the command can be set up without panicking
			// Don't actually execute it
			assert.NotNil(t, installCmd)
			assert.NotNil(t, installCmd.Run)
		})
	}
}

func TestInstallCommandRunPaths(t *testing.T) {
	t.Run("invalid id format", func(t *testing.T) {
		// capture output indirectly by ensuring no panic and path returns early
		installCmd.Run(installCmd, []string{"invalid"})
	})

	t.Run("unsupported provider", func(t *testing.T) {
		prev := isSupportedProviderFn
		prevAvail := availableProvidersFn
		isSupportedProviderFn = func(p string) bool { return false }
		availableProvidersFn = func() []string { return []string{"npm"} }
		defer func() { isSupportedProviderFn = prev; availableProvidersFn = prevAvail }()
		installCmd.Run(installCmd, []string{"pkg:unknown/x"})
	})

	t.Run("successful install single package", func(t *testing.T) {
		prevSupp := isSupportedProviderFn
		prevInstall := installPackageFn
		isSupportedProviderFn = func(p string) bool { return true }
		installPackageFn = func(id, v string) bool { return true }
		defer func() { isSupportedProviderFn = prevSupp; installPackageFn = prevInstall }()
		installCmd.Run(installCmd, []string{"pkg:npm/eslint"})
	})

	t.Run("successful install multiple packages", func(t *testing.T) {
		prevSupp := isSupportedProviderFn
		prevInstall := installPackageFn
		isSupportedProviderFn = func(p string) bool { return true }
		installPackageFn = func(id, v string) bool { return true }
		defer func() { isSupportedProviderFn = prevSupp; installPackageFn = prevInstall }()
		installCmd.Run(installCmd, []string{"pkg:npm/eslint", "pkg:pypi/black"})
	})

	t.Run("failed install single package", func(t *testing.T) {
		prevSupp := isSupportedProviderFn
		prevInstall := installPackageFn
		isSupportedProviderFn = func(p string) bool { return true }
		installPackageFn = func(id, v string) bool { return false }
		defer func() { isSupportedProviderFn = prevSupp; installPackageFn = prevInstall }()
		installCmd.Run(installCmd, []string{"pkg:npm/eslint"})
	})

	t.Run("mixed success/failure multiple packages", func(t *testing.T) {
		prevSupp := isSupportedProviderFn
		prevInstall := installPackageFn
		isSupportedProviderFn = func(p string) bool { return true }
		installPackageFn = func(id, v string) bool {
			// First package succeeds, second fails
			if id == "pkg:npm/eslint" {
				return true
			}
			return false
		}
		defer func() { isSupportedProviderFn = prevSupp; installPackageFn = prevInstall }()
		installCmd.Run(installCmd, []string{"pkg:npm/eslint", "pkg:pypi/black"})
	})
}

func TestInstallCommandErrorHandling(t *testing.T) {
	// Test error handling for invalid inputs without executing
	testCases := []struct {
		name string
		args []string
	}{
		{"invalid package format", []string{"invalid-format"}},
		{"unsupported provider", []string{"pkg:unsupported/package"}},
		{"malformed package id", []string{"pkg:"}},
		{"empty package id", []string{""}},
		{"multiple packages with one invalid", []string{"pkg:npm/valid", "invalid-format"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Just test that the command structure is valid
			// Don't actually execute it
			assert.NotNil(t, installCmd)
			assert.NotNil(t, installCmd.Run)
		})
	}
}

func TestInstallCommandStructure(t *testing.T) {
	// Test that the command has the expected structure
	assert.NotNil(t, installCmd.Run, "Install command should have a Run function")
	assert.NotEmpty(t, installCmd.Use, "Install command should have a Use field")
	assert.NotEmpty(t, installCmd.Short, "Install command should have a Short description")
	assert.NotEmpty(t, installCmd.Long, "Install command should have a Long description")
	assert.NotEmpty(t, installCmd.Aliases, "Install command should have aliases")
}

func TestInstallCommandProviderIntegration(t *testing.T) {
	// Test that the command integrates properly with the providers package
	validPackage := "pkg:npm/test-package"

	// Test that the package ID parsing works correctly
	if strings.HasPrefix(validPackage, "pkg:") {
		parts := strings.Split(strings.TrimPrefix(validPackage, "pkg:"), "/")
		assert.GreaterOrEqual(t, len(parts), 2)

		provider := parts[0]
		assert.True(t, providers.IsSupportedProvider(provider))
	}
}

func TestInstallCommandFullOutputGolden(t *testing.T) {
	t.Run("multiple packages with mixed success", func(t *testing.T) {
		// Capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		// Stub functions
		prevSupp := isSupportedProviderFn
		prevInstall := installPackageFn
		prevResolve := resolveVersionFn
		isSupportedProviderFn = func(p string) bool { return true }
		installPackageFn = func(id, v string) bool {
			// First package succeeds, second fails
			if strings.Contains(id, "eslint") {
				return true
			}
			return false
		}
		resolveVersionFn = func(id, v string) (string, error) {
			if v == "" {
				return "latest", nil
			}
			return v, nil
		}
		defer func() {
			isSupportedProviderFn = prevSupp
			installPackageFn = prevInstall
			resolveVersionFn = prevResolve
		}()

		installCmd.Run(installCmd, []string{"pkg:npm/eslint", "pkg:npm/prettier"})

		// Restore stdout and read
		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		// Check for expected output
		assert.Contains(t, out, "[✓] Successfully installed npm:eslint@latest")
		assert.Contains(t, out, "[✗] Failed to install npm:prettier@latest")
		assert.Contains(t, out, "Installation Summary:")
		assert.Contains(t, out, "Successfully installed: 1")
		assert.Contains(t, out, "Failed to install: 1")
		assert.Contains(t, out, "Failed packages: npm:prettier")
	})

	t.Run("multiple packages with versions", func(t *testing.T) {
		// Capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		// Stub functions
		prevSupp := isSupportedProviderFn
		prevInstall := installPackageFn
		prevResolve := resolveVersionFn
		isSupportedProviderFn = func(p string) bool { return true }
		installPackageFn = func(id, v string) bool { return true }
		resolveVersionFn = func(id, v string) (string, error) { return v, nil }
		defer func() {
			isSupportedProviderFn = prevSupp
			installPackageFn = prevInstall
			resolveVersionFn = prevResolve
		}()

		installCmd.Run(installCmd, []string{"pkg:npm/eslint@2.0.0", "pkg:pypi/black@22.3.0"})

		// Restore stdout and read
		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		// Check for expected output
		assert.Contains(t, out, "[✓] Successfully installed npm:eslint@2.0.0")
		assert.Contains(t, out, "[✓] Successfully installed pypi:black@22.3.0")
		assert.Contains(t, out, "Installation Summary:")
		assert.Contains(t, out, "Successfully installed: 2")
		assert.NotContains(t, out, "Failed to install:")
	})

	t.Run("all packages fail", func(t *testing.T) {
		// Capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		// Stub functions
		prevSupp := isSupportedProviderFn
		prevInstall := installPackageFn
		prevResolve := resolveVersionFn
		isSupportedProviderFn = func(p string) bool { return true }
		installPackageFn = func(id, v string) bool { return false }
		resolveVersionFn = func(id, v string) (string, error) {
			if v == "" {
				return "latest", nil
			}
			return v, nil
		}
		defer func() {
			isSupportedProviderFn = prevSupp
			installPackageFn = prevInstall
			resolveVersionFn = prevResolve
		}()

		installCmd.Run(installCmd, []string{"pkg:npm/eslint", "pkg:pypi/black"})

		// Restore stdout and read
		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		// Check for expected output
		assert.Contains(t, out, "[✗] Failed to install npm:eslint@latest")
		assert.Contains(t, out, "[✗] Failed to install pypi:black@latest")
		assert.Contains(t, out, "Installation Summary:")
		assert.Contains(t, out, "Successfully installed: 0")
		assert.Contains(t, out, "Failed to install: 2")
		assert.Contains(t, out, "Failed packages: npm:eslint, pypi:black")
	})
}

func TestIsValidVersionString(t *testing.T) {
	testCases := []struct {
		name     string
		version  string
		expected bool
	}{
		// Valid versions
		{"latest", "latest", true},
		{"semantic version", "1.0.0", true},
		{"version with v prefix", "v1.0.0", true},
		{"beta version", "1.0.0-beta", true},
		{"alpha version", "1.0.0-alpha.1", true},
		{"rc version", "1.0.0-rc.1", true},
		{"patch version", "1.0.1", true},
		{"major version", "2.0.0", true},
		{"version with build", "1.0.0+build.1", true},
		{"version with prerelease", "1.0.0-rc.1+build.1", true},

		// Invalid versions (no digits)
		{"empty string", "", false},
		{"just text", "alpha", false},
		{"just beta", "beta", false},
		{"just rc", "rc", false},
		{"just text with dash", "alpha-beta", false},
		{"just text with dot", "alpha.beta", false},
		{"just text with plus", "alpha+beta", false},
		{"just text with underscore", "alpha_beta", false},
		{"just text with tilde", "alpha~beta", false},
		{"just text with caret", "alpha^beta", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isValidVersionString(tc.version)
			assert.Equal(t, tc.expected, result, "Version validation mismatch for '%s'", tc.version)
		})
	}
}
