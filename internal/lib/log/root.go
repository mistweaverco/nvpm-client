package log

import (
	"log/slog"
	"os"
)

var logLevel slog.Level = slog.LevelDebug

const envDebug = "NVPM_DEBUG"
const envLogFormat = "NVPM_LOG_FORMAT"

func SetLogLevel(level slog.Level) {
	slog.SetLogLoggerLevel(level)
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
	// If the NVPM_DEBUG environment variable is set,
	if os.Getenv(envDebug) != "" {
		switch os.Getenv(envDebug) {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}
	}
	return slog.New(buildHandler(logLevel))
}
