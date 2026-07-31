package log

import (
	"log/slog"
	"testing"

	"github.com/mistweaverco/nvpm-client/internal/lib/version"
	"github.com/stretchr/testify/assert"
)

func TestLog(t *testing.T) {
	t.Run("set log level", func(t *testing.T) {
		SetLogLevel(slog.LevelInfo)
	})

	t.Run("new logger creation", func(t *testing.T) {
		t.Setenv("NVPM_DEBUG", "")
		logger := NewLogger()
		assert.NotNil(t, logger)
		assert.IsType(t, &slog.Logger{}, logger)
	})
}

func TestNewLoggerProductionSetsErrorLevel(t *testing.T) {
	prevVersion := version.VERSION
	prevLevel := logLevel
	defer func() {
		version.VERSION = prevVersion
		logLevel = prevLevel
	}()

	t.Setenv("NVPM_DEBUG", "")
	version.VERSION = "1.0.0"
	logger := NewLogger()
	assert.NotNil(t, logger)
	assert.Equal(t, slog.LevelError, logLevel)
}

func TestParseDebugEnv(t *testing.T) {
	level, ok := parseDebugEnv("")
	assert.False(t, ok)
	assert.Equal(t, slog.LevelError, level)

	level, ok = parseDebugEnv("debug")
	assert.True(t, ok)
	assert.Equal(t, slog.LevelDebug, level)

	level, ok = parseDebugEnv("1")
	assert.True(t, ok)
	assert.Equal(t, slog.LevelDebug, level)

	level, ok = parseDebugEnv("true")
	assert.True(t, ok)
	assert.Equal(t, slog.LevelDebug, level)

	level, ok = parseDebugEnv("info")
	assert.True(t, ok)
	assert.Equal(t, slog.LevelInfo, level)

	level, ok = parseDebugEnv("0")
	assert.True(t, ok)
	assert.Equal(t, slog.LevelError, level)
}

func TestNewLoggerDebugEnv(t *testing.T) {
	prev := logLevel
	defer func() { logLevel = prev }()

	t.Setenv("NVPM_DEBUG", "debug")
	_ = NewLogger()
	assert.True(t, DebugEnabled())
	assert.True(t, VerboseEnabled())
}
