package nvpm

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/providers"
	"github.com/stretchr/testify/assert"
)

func TestHealthCommand(t *testing.T) {
	t.Run("health command structure", func(t *testing.T) {
		assert.Equal(t, "health", healthCmd.Use)
		assert.Equal(t, "Check system health and requirements", healthCmd.Short)
		assert.NotEmpty(t, healthCmd.Long)
		// Note: We can't easily test Args since it's a function type
	})

	t.Run("health command has no subcommands", func(t *testing.T) {
		assert.Empty(t, healthCmd.Commands())
	})
}

func TestHealthCommandRun(t *testing.T) {
	t.Run("prints all provider statuses and overall", func(t *testing.T) {
		// stub provider health checks
		prev := checkAllProvidersHealthFn
		checkAllProvidersHealthFn = func() []providers.ProviderHealthStatus {
			return []providers.ProviderHealthStatus{
				{Provider: "npm", Available: true, Description: "Node.js package manager"},
				{Provider: "pypi", Available: false, RequiredTool: "pip3", Description: "Python package manager"},
				{Provider: "golang", Available: true, Description: "Go programming language"},
				{Provider: "generic", Available: true, Description: "Generic provider"},
			}
		}
		defer func() { checkAllProvidersHealthFn = prev }()

		// capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		healthCmd.Run(healthCmd, []string{})

		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		// Check that all providers are listed
		assert.Contains(t, out, "NPM")
		assert.Contains(t, out, "PYPI")
		assert.Contains(t, out, "GOLANG")
		assert.Contains(t, out, "GENERIC")

		// Check available providers show "Available"
		assert.Contains(t, out, "NPM: Available")
		assert.Contains(t, out, "GOLANG: Available")
		assert.Contains(t, out, "GENERIC: Available")

		// Check unavailable provider shows warning and missing tool
		assert.Contains(t, out, "PYPI")
		assert.Contains(t, out, "Not available (missing: pip3)")
		assert.Contains(t, out, "Python package manager") // Description should be shown for unavailable

		// Check overall warning message
		assert.Contains(t, out, "Some providers are not available")
	})

	t.Run("prints success message when all providers available", func(t *testing.T) {
		// stub provider health checks - all available
		prev := checkAllProvidersHealthFn
		checkAllProvidersHealthFn = func() []providers.ProviderHealthStatus {
			return []providers.ProviderHealthStatus{
				{Provider: "npm", Available: true, Description: "Node.js package manager"},
				{Provider: "pypi", Available: true, Description: "Python package manager"},
				{Provider: "generic", Available: true, Description: "Generic provider"},
			}
		}
		defer func() { checkAllProvidersHealthFn = prev }()

		// capture stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		healthCmd.Run(healthCmd, []string{})

		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		out := buf.String()

		// When not in TTY (tests), icons use plain text format [✓]
		assert.Contains(t, out, "[✓] All providers are available! Your system is ready to use NVPM.")
		assert.NotContains(t, out, "Some providers are not available")

		// All providers should show as Available
		assert.Contains(t, out, "NPM: Available")
		assert.Contains(t, out, "PYPI: Available")
		assert.Contains(t, out, "GENERIC: Available")
	})
}
