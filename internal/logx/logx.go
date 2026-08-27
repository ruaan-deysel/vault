// Package logx provides centralized, runtime-adjustable leveled logging using
// log/slog. It bridges Go's standard log package output into a shared slog
// handler so that existing log.Print* calls respect the runtime log level
// without requiring changes across all call sites.
//
// This package is intentionally kept free of database or settings dependencies.
package logx

import (
	"io"
	"log/slog"
	"strings"
	"time"
)

// levelVar is the single source of truth for the active log level across the process.
// It initializes to slog.LevelInfo (zero value).
var levelVar = new(slog.LevelVar)

// Setup initializes the global default logger with a text handler writing to w
// at the level governed by levelVar. Standard library log output is routed
// through this handler at Info level.
func Setup(w io.Writer) {
	opts := &slog.HandlerOptions{
		Level:       levelVar,
		ReplaceAttr: replaceAttr,
	}
	handler := slog.NewTextHandler(w, opts)
	slog.SetDefault(slog.New(handler))
}

// replaceAttr formats record attributes to ensure log lines stay clean,
// readable, and compatible with diagnostics collection.
func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		if t := a.Value.Time(); !t.IsZero() {
			return slog.String(slog.TimeKey, t.Format(time.RFC3339))
		}
	}
	return a
}

// SetLevelString updates the active log level from a case-insensitive string name.
// Allowed values are "debug", "info", "warn" (or "warning"), and "error".
// Any unknown or empty value safely falls back to Info.
func SetLevelString(s string) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		levelVar.Set(slog.LevelDebug)
	case "info":
		levelVar.Set(slog.LevelInfo)
	case "warn", "warning":
		levelVar.Set(slog.LevelWarn)
	case "error":
		levelVar.Set(slog.LevelError)
	default:
		levelVar.Set(slog.LevelInfo)
	}
}

// Level returns the current active log level.
func Level() slog.Level {
	return levelVar.Level()
}

// LevelString returns the current active log level as a lowercase string
// ("debug", "info", "warn", "error").
func LevelString() string {
	switch levelVar.Level() {
	case slog.LevelDebug:
		return "debug"
	case slog.LevelInfo:
		return "info"
	case slog.LevelWarn:
		return "warn"
	case slog.LevelError:
		return "error"
	default:
		return "info"
	}
}
