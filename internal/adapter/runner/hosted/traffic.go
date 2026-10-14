package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/youssefmahersi/ballast/internal/infra/egress"
)

// stopGrace is how long the proxy is given to write its last totals before it
// is killed.
//
// Stopping it rather than killing it outright is the whole of how the figure is
// collected: on SIGTERM the proxy writes one last line, and that line is the
// one read below. Two seconds is far more than printing a number takes, and the
// container is removed either way.
const stopGrace = 2 * time.Second

// trafficOf stops the proxy and reads what it moved.
//
// Called once the generator has finished, which matters: by then its
// connections are closed, so no byte is still in flight and the proxy's last
// line is its final word.
//
// A figure that cannot be read is zero and said so — never a guess. It is
// summed into what an organization is charged for, and an invented number there
// is worse than a missing one.
func (r *Runner) trafficOf(ctx context.Context, proxyID string) (egressTraffic, error) {
	grace := int(stopGrace.Seconds())
	if err := r.cli.ContainerStop(ctx, proxyID, container.StopOptions{Timeout: &grace}); err != nil {
		return egressTraffic{}, fmt.Errorf("stop the egress proxy: %w", err)
	}

	logs, err := r.cli.ContainerLogs(ctx, proxyID, container.LogsOptions{ShowStdout: true})
	if err != nil {
		return egressTraffic{}, fmt.Errorf("read the egress proxy's output: %w", err)
	}
	defer func() { _ = logs.Close() }()

	// Docker multiplexes the two streams onto one connection, with a header per
	// chunk. Only stdout carries the totals; stderr is discarded rather than
	// mixed into lines meant to be parsed.
	var out bytes.Buffer
	if _, err := stdcopy.StdCopy(&out, io.Discard, logs); err != nil {
		return egressTraffic{}, fmt.Errorf("read the egress proxy's output: %w", err)
	}

	return lastTotals(out.Bytes())
}

// egressTraffic is what the proxy moved, in bytes.
type egressTraffic struct {
	Sent     int64
	Received int64
}

// lastTotals reads the final totals out of the proxy's output.
//
// The last line wins: the proxy writes a running total every few seconds and
// one more when it is asked to stop, so earlier lines are earlier answers to
// the same question rather than separate facts to add up.
func lastTotals(output []byte) (egressTraffic, error) {
	var (
		found   bool
		traffic egressTraffic
	)

	for _, line := range bytes.Split(output, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var entry struct {
			Message  string `json:"msg"`
			Sent     *int64 `json:"sent_bytes"`
			Received *int64 `json:"received_bytes"`
		}
		// A line that is not JSON is not a failure: the proxy's own startup
		// messages share this stream, and so would anything the runtime
		// printed.
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Message != egress.TrafficMessage || entry.Sent == nil || entry.Received == nil {
			continue
		}
		if *entry.Sent < 0 || *entry.Received < 0 {
			continue
		}

		traffic = egressTraffic{Sent: *entry.Sent, Received: *entry.Received}
		found = true
	}

	if !found {
		return egressTraffic{}, fmt.Errorf("the egress proxy reported no totals")
	}

	return traffic, nil
}
