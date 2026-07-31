package log

import (
	"log/slog"
	"os"
	"strings"
)

var logLevel slog.Level = slog.LevelDebug

const envDebug = "NVPM_DEBUG"
const envLogFormat = "NVPM_LOG_FORMAT"

func SetLogLevel(level slog.Level) {
	logLevel = level
	slog.SetLogLoggerLevel(level)
}

// Level returns the active slog level chosen from NVPM_DEBUG / defaults.
func Level() slog.Level {
	return logLevel
}

// DebugEnabled reports whether verbose debug logging is on (NVPM_DEBUG=debug|true|1|…).
func DebugEnabled() bool {
	return logLevel <= slog.LevelDebug
}

// VerboseEnabled reports whether info-or-finer logging is on.
func VerboseEnabled() bool {
	return logLevel <= slog.LevelInfo
}

func parseDebugEnv(raw string) (level slog.Level, set bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return slog.LevelError, false
	}
	switch raw {
	case "0", "false", "no", "off", "error":
		return slog.LevelError, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "info":
		return slog.LevelInfo, true
	case "debug", "1", "true", "yes", "on":
		return slog.LevelDebug, true
	default:
		// Any other non-empty value (e.g. NVPM_DEBUG=trace) enables debug.
		return slog.LevelDebug, true
	}
}

func buildHandler(level slog.Level) slog.Handler {
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// Keep CLI errors compact and readable by omitting timestamps.
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}
	if os.Getenv(envLogFormat) == "json" {
		return slog.NewJSONHandler(os.Stderr, opts)
	}
	return slog.NewTextHandler(os.Stderr, opts)
}

func NewLogger() *slog.Logger {
	logLevel = slog.LevelError
	if level, ok := parseDebugEnv(os.Getenv(envDebug)); ok {
		logLevel = level
	}
	return slog.New(buildHandler(logLevel))
}
