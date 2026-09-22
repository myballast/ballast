package hosted

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// maxLineBytes caps a single line. A script that prints without ever writing a
// newline would otherwise be held in memory in full, and a k6 script is code
// somebody uploaded.
const maxLineBytes = 64 << 10

// stream follows the container's output and forwards it line by line.
//
// The returned channel closes once the output is exhausted, which is how Run
// knows every line has been delivered before it removes the container.
func (r *Runner) stream(ctx context.Context, id string, logs chan<- domain.LogLine) <-chan struct{} {
	done := make(chan struct{})

	if logs == nil {
		close(done)
		return done
	}

	go func() {
		defer close(done)

		reader, err := r.cli.ContainerLogs(ctx, id, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
		})
		if err != nil {
			return
		}
		defer func() { _ = reader.Close() }()

		var seq atomic.Int64
		stdout := &lineWriter{ctx: ctx, logs: logs, stream: domain.StreamStdout, seq: &seq}
		stderr := &lineWriter{ctx: ctx, logs: logs, stream: domain.StreamStderr, seq: &seq}

		// Docker multiplexes both streams onto one connection, with a header
		// per chunk. StdCopy is what pulls them apart again.
		_, _ = stdcopy.StdCopy(stdout, stderr, reader)

		stdout.flush()
		stderr.flush()
	}()

	return done
}

// lineWriter turns a byte stream into whole lines. Docker hands over whatever
// chunks happened to arrive, which cut across line boundaries.
type lineWriter struct {
	ctx    context.Context
	logs   chan<- domain.LogLine
	stream string
	// Shared between the two writers, so that the interleaving of stdout and
	// stderr is preserved in the order the lines are stored.
	seq *atomic.Int64
	buf []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)

	for {
		end := bytes.IndexByte(w.buf, '\n')
		if end < 0 {
			break
		}
		w.emit(string(w.buf[:end]))
		w.buf = w.buf[end+1:]
	}

	// No newline in sight and the buffer is already too big: cut it and move
	// on rather than grow without bound.
	for len(w.buf) >= maxLineBytes {
		w.emit(string(w.buf[:maxLineBytes]))
		w.buf = w.buf[maxLineBytes:]
	}

	return len(p), nil
}

// flush emits whatever the container left without a trailing newline.
func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.emit(string(w.buf))
		w.buf = nil
	}
}

func (w *lineWriter) emit(line string) {
	entry := domain.LogLine{
		Seq:     w.seq.Add(1) - 1,
		Stream:  w.stream,
		Message: strings.TrimRight(line, "\r"),
		At:      time.Now().UTC(),
	}

	// A caller that stopped reading must not wedge the run.
	select {
	case w.logs <- entry:
	case <-w.ctx.Done():
	}
}
