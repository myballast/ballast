package logging

import (
	"log/slog"
	"os"
)

// New returns the structured logger the processes share.
//
// Output is JSON on stdout: a run's logs are read by machines before they are
// read by people. Nothing that passes through here may carry a secret, a token,
// an Authorization header or a Cookie.
func New(service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})

	return slog.New(handler).With("service", service)
}
