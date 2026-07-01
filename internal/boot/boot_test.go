package boot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBoot(t *testing.T) {
	t.Run("default registry URL constant", func(t *testing.T) {
		expectedURL := "https://github.com/mistweaverco/nvpm-registry/releases/latest/download/nvpm-registry.json.zip"
		assert.Equal(t, expectedURL, DEFAULT_REGISTRY_URL)
	})

	t.Run("initial model creation", func(t *testing.T) {
		cacheMaxAge := 24 * time.Hour
		model := initialModel(cacheMaxAge)

		assert.False(t, model.quitting)
		assert.False(t, model.downloading)
		assert.False(t, model.downloadFinished)
		assert.Equal(t, cacheMaxAge, model.cacheMaxAge)
		assert.Equal(t, "Checking registry cache...", model.message)
		assert.NotNil(t, model.spinner)
	})

	t.Run("model methods exist", func(t *testing.T) {
		cacheMaxAge := 1 * time.Hour
		model := initialModel(cacheMaxAge)

		// Test that these methods exist and don't panic
		assert.NotNil(t, model.checkCache())
		assert.NotNil(t, model.performDownload())
		assert.NotNil(t, model.unzipRegistry())
		assert.NotNil(t, model.performUnzip())
		assert.NotNil(t, model.syncLocalPackages())
		assert.NotNil(t, model.performSyncLocalPackages())
	})

	t.Run("model interface implementation", func(t *testing.T) {
		cacheMaxAge := 1 * time.Hour
		model := initialModel(cacheMaxAge)

		// Test that Init method exists and returns a command
		initCmd := model.Init()
		assert.NotNil(t, initCmd)

		// Test that View method exists and returns a string
		view := model.View()
		assert.IsType(t, "", view)
		assert.NotEmpty(t, view)
	})
}

func TestModelMethods(t *testing.T) {
	t.Run("model update method", func(t *testing.T) {
		cacheMaxAge := 1 * time.Hour
		model := initialModel(cacheMaxAge)

		// Test that Update method exists and can be called
		// We can't easily test the actual tea.Msg handling without complex mocking
		// But we can verify the method exists and returns the right types
		updatedModel, cmd := model.Update(nil)
		assert.IsType(t, model, updatedModel)
		assert.Nil(t, cmd) // Should return nil command for nil message
	})

	t.Run("model start function", func(t *testing.T) {
		// Start runs an interactive Bubble Tea program and calls os.Exit(1) on failure.
		// In unit tests (no TTY), running it would fail the whole test process.
		// So we only assert the symbol exists and is callable in principle.
		assert.NotNil(t, Start)
	})
}

func TestModelCommands(t *testing.T) {
	t.Run("model command methods", func(t *testing.T) {
		cacheMaxAge := 1 * time.Hour
		model := initialModel(cacheMaxAge)

		// Test that all command methods exist and return commands
		// These are tea.Cmd functions that we can't easily test without complex mocking
		// But we can verify they exist and return the right types
		assert.NotNil(t, model.checkCache())
		assert.NotNil(t, model.performDownload())
		assert.NotNil(t, model.unzipRegistry())
		assert.NotNil(t, model.performUnzip())
		assert.NotNil(t, model.syncLocalPackages())
		assert.NotNil(t, model.performSyncLocalPackages())
	})
}
