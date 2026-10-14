//intenza:paradigm imperative-shell

// Package logx builds the structured JSON logger every binary uses.
package logx

import (
	"log/slog"
	"os"
)

// New returns a JSON logger tagged with the service name.
func New(service string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})).With("service", service)
}
