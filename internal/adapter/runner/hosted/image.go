package hosted

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// ensureImage makes sure the generator image is on the machine before a
// container is asked for.
//
// Docker does not fetch a missing image on its own: asking it to create a
// container from one it has never seen fails with "No such image". A fresh
// worker, or a fresh continuous integration runner, has seen none of them.
//
// The tag is pinned, so once the image is here it is here for good and nothing
// is fetched again. That also means a run never silently picks up a different
// generator than the one before it.
func (r *Runner) ensureImage(ctx context.Context) error {
	r.imageOnce.Lock()
	defer r.imageOnce.Unlock()

	if r.imagePresent {
		return nil
	}

	if _, _, err := r.cli.ImageInspectWithRaw(ctx, r.cfg.Image); err == nil {
		r.imagePresent = true
		return nil
	} else if !client.IsErrNotFound(err) {
		return fmt.Errorf("look for the image %s: %w", r.cfg.Image, err)
	}

	progress, err := r.cli.ImagePull(ctx, r.cfg.Image, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull the image %s: %w", r.cfg.Image, err)
	}
	defer func() { _ = progress.Close() }()

	// The stream has to be read to the end. Closing it early cancels the
	// download, and the image is left unusable — the failure then looks exactly
	// like the one this function exists to prevent.
	if _, err := io.Copy(io.Discard, progress); err != nil {
		return fmt.Errorf("pull the image %s: %w", r.cfg.Image, err)
	}

	// The daemon reports a pull as finished before the image is necessarily
	// resolvable by tag, so this both confirms and records it.
	if _, _, err := r.cli.ImageInspectWithRaw(ctx, r.cfg.Image); err != nil {
		return fmt.Errorf("the image %s is still missing after pulling it: %w", r.cfg.Image, err)
	}

	r.imagePresent = true
	return nil
}

// imageGuard serialises the check above. Two runs starting at once must not both
// pull, and the second must not proceed on a half-downloaded image.
type imageGuard = sync.Mutex
