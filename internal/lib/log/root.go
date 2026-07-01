package log

import (
	"log/slog"
	"os"
)

var logLevel slog.Level = slog.LevelDebug

func SetLogLevel(level slog.Level) {
	slog.SetLogLoggerLevel(level)
}

func NewLogger() *slog.Logger {
	logLevel = slog.LevelError
	// If the NVPM_DEBUG environment variable is set,
	if os.Getenv("NVPM_DEBUG") != "" {
		switch os.Getenv("NVPM_DEBUG") {
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
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
}
