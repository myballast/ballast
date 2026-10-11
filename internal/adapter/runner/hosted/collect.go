package hosted

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// Compile-time proof that this runner is also what cleans up after one.
var _ run.Containers = (*Runner)(nil)

// Destroy removes everything one run left on this machine: its generator, its
// proxy, and the network they lived on.
//
// Finding nothing is not an error. Two collectors cleaning up after the same
// dead worker are doing the same work, and the second must not report a failure
// for having been slower.
func (r *Runner) Destroy(ctx context.Context, runID uuid.UUID) error {
	wanted := runID.String()

	_, err := r.destroy(ctx, func(labelled string) bool { return labelled == wanted })

	return err
}

// DestroyExcept removes everything belonging to a run that is not in keep.
//
// What a worker calls when it starts. Everything it cannot account for goes:
// the label says a run owns this container, and if that run is not one this
// worker was told is alive, then nobody is watching what the container is
// doing.
//
// A label this cannot parse is not a reason to spare anything. A container
// claiming to belong to a run whose identifier is not a run identifier is
// exactly the kind of leftover worth removing.
func (r *Runner) DestroyExcept(ctx context.Context, keep []uuid.UUID) (int, error) {
	spared := make(map[string]struct{}, len(keep))
	for _, id := range keep {
		spared[id.String()] = struct{}{}
	}

	return r.destroy(ctx, func(labelled string) bool {
		_, alive := spared[labelled]
		return !alive
	})
}

// destroy removes every container, and then every network, whose run label
// satisfies doomed. It reports how many containers it removed.
//
// Containers first, because a network with something attached cannot be
// removed. Failures are gathered rather than returned at the first one: a
// leftover that cannot be removed must not protect the next one.
func (r *Runner) destroy(ctx context.Context, doomed func(runID string) bool) (int, error) {
	ours := filters.NewArgs(filters.Arg("label", RunIDLabel))

	// All, not just the running ones: a stopped container still holds its
	// network, its writable layer and its logs.
	found, err := r.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: ours})
	if err != nil {
		return 0, fmt.Errorf("list the run containers: %w", err)
	}

	var (
		removed  int
		failures []error
	)
	for _, existing := range found {
		if !doomed(existing.Labels[RunIDLabel]) {
			continue
		}

		err := r.cli.ContainerRemove(ctx, existing.ID, container.RemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		})
		switch {
		case err == nil:
			removed++

		case client.IsErrNotFound(err), errdefs.IsConflict(err):
			// Already gone, or already on its way out in somebody else's
			// hands — the daemon answers the second remover with a conflict.
			// Two collectors cleaning up after one dead worker are doing the
			// same work, and either way the container has stopped generating
			// load, which is what the caller is about to assert.

		default:
			failures = append(failures, fmt.Errorf("remove the container %s: %w", existing.ID, err))
		}
	}

	networks, err := r.cli.NetworkList(ctx, network.ListOptions{Filters: ours})
	if err != nil {
		failures = append(failures, fmt.Errorf("list the run networks: %w", err))
		return removed, errors.Join(failures...)
	}

	for _, existing := range networks {
		if !doomed(existing.Labels[RunIDLabel]) {
			continue
		}

		if err := r.removeNetwork(ctx, existing.ID); err != nil {
			failures = append(failures, fmt.Errorf("remove the network %s: %w", existing.Name, err))
		}
	}

	return removed, errors.Join(failures...)
}

// removeNetwork removes one run network, retrying briefly.
//
// Removing a container tells the daemon to detach its endpoint; the endpoint is
// gone a moment later, and in that moment the network still has one and refuses
// to be removed. Retrying costs a few hundred milliseconds in a collector that
// runs twice a minute, and spares the run it is cleaning up a whole interval of
// waiting to be ended.
func (r *Runner) removeNetwork(ctx context.Context, id string) error {
	const attempts = 3

	var err error
	for attempt := range attempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}

		err = r.cli.NetworkRemove(ctx, id)
		if err == nil || client.IsErrNotFound(err) {
			return nil
		}
	}

	return err
}
