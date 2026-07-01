package log

import (
	"log/slog"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/version"
	"github.com/stretchr/testify/assert"
)

func TestLog(t *testing.T) {
	t.Run("set log level", func(t *testing.T) {
		// Test that SetLogLevel doesn't panic
		SetLogLevel(slog.LevelInfo)
		// We can't easily test the actual log level change without capturing slog output
		// But we can verify the function exists and is callable
	})

	t.Run("new logger creation", func(t *testing.T) {
		logger := NewLogger()
		assert.NotNil(t, logger)
		assert.IsType(t, &slog.Logger{}, logger)
	})

	t.Run("log level variable exists", func(t *testing.T) {
		// The logLevel variable should exist and be accessible
		// We can't directly access it from outside the package, but we can test it indirectly
		logger := NewLogger()
		assert.NotNil(t, logger)
	})
}

func TestNewLoggerProductionSetsErrorLevel(t *testing.T) {
	prevVersion := version.VERSION
	prevLevel := logLevel
	defer func() {
		version.VERSION = prevVersion
		logLevel = prevLevel
	}()

	version.VERSION = "1.0.0"
	logger := NewLogger()
	assert.NotNil(t, logger)
	assert.Equal(t, slog.LevelError, logLevel)
}
