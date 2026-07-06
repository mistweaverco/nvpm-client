package nvpm

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoveCommand(t *testing.T) {
	t.Run("rm command structure", func(t *testing.T) {
		assert.Equal(t, "rm <pkgId> [pkgId...]", rmCmd.Use)
		assert.Equal(t, "Removes one or more packages", rmCmd.Short)
		assert.NotEmpty(t, rmCmd.Long)
		// Note: We can't easily test Args since it's a function type
		assert.Contains(t, rmCmd.Aliases, "remove")
		assert.Contains(t, rmCmd.Aliases, "delete")
	})

	t.Run("remove command has no subcommands", func(t *testing.T) {
		assert.Empty(t, rmCmd.Commands())
	})
}

func TestRemoveCommandRunPaths(t *testing.T) {
	t.Run("invalid id format", func(t *testing.T) {
		rmCmd.Run(rmCmd, []string{"invalid"})
	})

	t.Run("unsupported provider", func(t *testing.T) {
		prevSupp := isSupportedProviderFn
		prevAvail := availableProvidersFn
		isSupportedProviderFn = func(p string) bool { return false }
		availableProvidersFn = func() []string { return []string{"npm"} }
		defer func() { isSupportedProviderFn = prevSupp; availableProvidersFn = prevAvail }()
		rmCmd.Run(rmCmd, []string{"pkg:unknown/x"})
	})

	t.Run("successful remove single package", func(t *testing.T) {
		prevSupp := isSupportedProviderFn
		prevRemove := removePackageFn
		isSupportedProviderFn = func(p string) bool { return true }
		removePackageFn = func(id string) bool { return true }
		defer func() { isSupportedProviderFn = prevSupp; removePackageFn = prevRemove }()
		rmCmd.Run(rmCmd, []string{"pkg:npm/eslint"})
	})

	t.Run("successful remove multiple packages", func(t *testing.T) {
		prevSupp := isSupportedProviderFn
		prevRemove := removePackageFn
		isSupportedProviderFn = func(p string) bool { return true }
		removePackageFn = func(id string) bool { return true }
		defer func() { isSupportedProviderFn = prevSupp; removePackageFn = prevRemove }()
		rmCmd.Run(rmCmd, []string{"pkg:npm/eslint", "pkg:pypi/black"})
	})

	t.Run("failed remove single package", func(t *testing.T) {
		prevSupp := isSupportedProviderFn
		prevRemove := removePackageFn
		isSupportedProviderFn = func(p string) bool { return true }
		removePackageFn = func(id string) bool { return false }
		defer func() { isSupportedProviderFn = prevSupp; removePackageFn = prevRemove }()
		rmCmd.Run(rmCmd, []string{"pkg:npm/eslint"})
	})

	t.Run("mixed success/failure multiple packages", func(t *testing.T) {
		prevSupp := isSupportedProviderFn
		prevRemove := removePackageFn
		isSupportedProviderFn = func(p string) bool { return true }
		removePackageFn = func(id string) bool {
			// First package succeeds, second fails
			if id == "pkg:npm/eslint" {
				return true
			}
			return false
		}
		defer func() { isSupportedProviderFn = prevSupp; removePackageFn = prevRemove }()
		rmCmd.Run(rmCmd, []string{"pkg:npm/eslint", "pkg:pypi/black"})
	})
}

func TestRemoveCommandFullOutputGolden(t *testing.T) {
	t.Run("remove success single package", func(t *testing.T) {
		// Capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		// Stub functions
		prevSupp := isSupportedProviderFn
		prevRemove := removePackageFn
		isSupportedProviderFn = func(p string) bool { return true }
		removePackageFn = func(id string) bool { return true }
		defer func() { isSupportedProviderFn = prevSupp; removePackageFn = prevRemove }()

		rmCmd.Run(rmCmd, []string{"pkg:npm/eslint"})

		// Restore stdout and read
		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		assert.Contains(t, out, "Removing 1 package(s)")
		assert.Contains(t, out, "[✓] Successfully removed npm:eslint")
		assert.Contains(t, out, "All packages removed successfully!")
	})

	t.Run("remove success multiple packages", func(t *testing.T) {
		// Capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		// Stub functions
		prevSupp := isSupportedProviderFn
		prevRemove := removePackageFn
		isSupportedProviderFn = func(p string) bool { return true }
		removePackageFn = func(id string) bool { return true }
		defer func() { isSupportedProviderFn = prevSupp; removePackageFn = prevRemove }()

		rmCmd.Run(rmCmd, []string{"pkg:npm/eslint", "pkg:pypi/black"})

		// Restore stdout and read
		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		assert.Contains(t, out, "Removing 2 package(s)")
		assert.Contains(t, out, "[✓] Successfully removed npm:eslint")
		assert.Contains(t, out, "[✓] Successfully removed pypi:black")
		assert.Contains(t, out, "All packages removed successfully!")
	})

	t.Run("remove failure single package", func(t *testing.T) {
		// Capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		// Stub functions
		prevSupp := isSupportedProviderFn
		prevRemove := removePackageFn
		isSupportedProviderFn = func(p string) bool { return true }
		removePackageFn = func(id string) bool { return false }
		defer func() { isSupportedProviderFn = prevSupp; removePackageFn = prevRemove }()

		rmCmd.Run(rmCmd, []string{"pkg:npm/eslint"})

		// Restore stdout and read
		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		assert.Contains(t, out, "Removing 1 package(s)")
		assert.Contains(t, out, "[✗] Failed to remove npm:eslint")
		assert.Contains(t, out, "Some packages failed to remove.")
	})

	t.Run("remove mixed success/failure multiple packages", func(t *testing.T) {
		// Capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		// Stub functions
		prevSupp := isSupportedProviderFn
		prevRemove := removePackageFn
		isSupportedProviderFn = func(p string) bool { return true }
		removePackageFn = func(id string) bool {
			// First package succeeds, second fails
			if id == "npm:eslint" {
				return true
			}
			return false
		}
		defer func() { isSupportedProviderFn = prevSupp; removePackageFn = prevRemove }()

		rmCmd.Run(rmCmd, []string{"pkg:npm/eslint", "pkg:pypi/black"})

		// Restore stdout and read
		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		assert.Contains(t, out, "Removing 2 package(s)")
		assert.Contains(t, out, "[✓] Successfully removed npm:eslint")
		assert.Contains(t, out, "[✗] Failed to remove pypi:black")
		assert.Contains(t, out, "Some packages failed to remove.")
	})
}
