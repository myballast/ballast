//go:build integration

// Inside the package rather than beside it: what needs testing here is the
// check that runs before a container is created, and the flag that keeps it
// from running twice. Neither is part of the runner's public surface, and
// neither should be.
package hosted

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// A deliberately tiny image — around twenty kilobytes. The generator image is
// seventy-four megabytes, and a test that deleted it would make every local run
// download it again. What is under test is the fetching, not the image.
const throwawayImage = "hello-world:latest"

func newRunnerFor(t *testing.T, ref string) *Runner {
	t.Helper()

	runner, err := New(Config{Image: ref})
	if err != nil {
		t.Fatalf("connect to docker: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })

	return runner
}

func imageIsPresent(ctx context.Context, t *testing.T, cli client.APIClient, ref string) bool {
	t.Helper()

	_, _, err := cli.ImageInspectWithRaw(ctx, ref)
	if err == nil {
		return true
	}
	if client.IsErrNotFound(err) {
		return false
	}

	t.Fatalf("look for %s: %v", ref, err)
	return false
}

// removeImage clears the ground. It tolerates failure: another test, or the
// person running this, may hold a container made from it.
func removeImage(ctx context.Context, t *testing.T, cli client.APIClient, ref string) {
	t.Helper()

	if _, err := cli.ImageRemove(ctx, ref, image.RemoveOptions{
		Force:         true,
		PruneChildren: true,
	}); err != nil && !client.IsErrNotFound(err) {
		t.Skipf("cannot remove %s to set the test up: %v", ref, err)
	}
}

// The regression this covers: the runner used to assume the image was already
// on the machine. It was, on the machine where the tests were written, and it
// never is on a fresh one.
func TestEnsureImagePullsWhenItIsMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner := newRunnerFor(t, throwawayImage)

	removeImage(ctx, t, runner.cli, throwawayImage)
	if imageIsPresent(ctx, t, runner.cli, throwawayImage) {
		t.Skipf("%s could not be removed, so this test would prove nothing", throwawayImage)
	}

	if err := runner.ensureImage(ctx); err != nil {
		t.Fatalf("ensureImage: %v", err)
	}

	if !imageIsPresent(ctx, t, runner.cli, throwawayImage) {
		t.Error("ensureImage reported success but the image is still absent")
	}
	if !runner.imagePresent {
		t.Error("the runner did not record that the image is now present")
	}
}

// Once is once. The tag is pinned, so a second run must not go back to the
// registry — which is what makes a hundred runs cost one download.
func TestEnsureImageDoesNothingTheSecondTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner := newRunnerFor(t, throwawayImage)

	if err := runner.ensureImage(ctx); err != nil {
		t.Fatalf("first ensureImage: %v", err)
	}

	// Taking the image away again is how the second call is caught going back
	// to the daemon: if it did, it would notice and pull, and the image would
	// be here. It must instead return on the flag alone.
	removeImage(ctx, t, runner.cli, throwawayImage)
	if imageIsPresent(ctx, t, runner.cli, throwawayImage) {
		t.Skipf("%s could not be removed, so this test would prove nothing", throwawayImage)
	}

	if err := runner.ensureImage(ctx); err != nil {
		t.Fatalf("second ensureImage: %v", err)
	}
	if imageIsPresent(ctx, t, runner.cli, throwawayImage) {
		t.Error("the second call fetched the image again instead of trusting what it already knew")
	}
}

// An image that does not exist has to fail, and say so. Silence here is how a
// run ends up waiting on a container that will never start.
func TestEnsureImageFailsOnAnImageThatDoesNotExist(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner := newRunnerFor(t, "ballast-no-such-image:0.0.0-does-not-exist")

	err := runner.ensureImage(ctx)
	if err == nil {
		t.Fatal("ensureImage accepted an image that cannot be fetched")
	}
	if !strings.Contains(err.Error(), "ballast-no-such-image") {
		t.Errorf("the error does not name the image: %v", err)
	}
	if runner.imagePresent {
		t.Error("the runner recorded a missing image as present")
	}
}
