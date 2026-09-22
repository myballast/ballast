package domain

import "time"

// LogLine is one line a run's generator wrote.
//
// Seq orders the lines within a run: it is what the live log view follows, and
// what makes the order reproducible when several lines share a timestamp.
type LogLine struct {
	Seq int64
	// Stream is "stdout" or "stderr", the two values run_log accepts.
	Stream  string
	Message string
	At      time.Time
}

// The two streams a generator writes to.
const (
	StreamStdout = "stdout"
	StreamStderr = "stderr"
)
