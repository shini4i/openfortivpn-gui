// Package logging provides structured logging setup using log/slog.
package logging

import (
	"log/slog"
	"os"
)

// Level represents the logging verbosity level.
type Level int

const (
	// LevelInfo is the default logging level for normal operation.
	LevelInfo Level = iota
	// LevelDebug enables verbose debug output.
	LevelDebug
)

// Setup initializes the global slog logger with the specified level.
// Call this once at application startup.
func Setup(level Level) {
	var slogLevel slog.Level
	switch level {
	case LevelDebug:
		slogLevel = slog.LevelDebug
	default:
		slogLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: slogLevel,
	}

	handler := slog.NewTextHandler(os.Stderr, opts)
	logger := slog.New(handler)
	slog.SetDefault(logger)
}

// SetupFromEnv initializes the logger based on environment variables.
// Set OPENFORTIVPN_GUI_DEBUG=1 to enable debug logging.
func SetupFromEnv() {
	level := LevelInfo
	if os.Getenv("OPENFORTIVPN_GUI_DEBUG") == "1" {
		level = LevelDebug
	}
	Setup(level)
}
