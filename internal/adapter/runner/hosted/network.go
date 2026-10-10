package hosted

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
)

// NetworkPrefix is how a run's network is named. The garbage collector looks
// for it, and so does anybody wondering what is on their machine.
const NetworkPrefix = "ballast-run-"

// NewRunNetwork creates the network one run lives on, and returns the name
// along with the function that removes it.
//
// Internal, which is the whole point: Docker gives such a network no gateway, so
// a container on it has no default route and no amount of configuration inside
// the container puts one there. The generator is attached to this and nothing
// else, so the proxy is not merely the recommended way out — it is the only one
// that exists.
//
// Exported because the tests need a network before a run starts, to attach a
// target container to it, and because the collector that sweeps up after a
// killed worker needs to recognise one.
func (r *Runner) NewRunNetwork(ctx context.Context, runID uuid.UUID) (string, func(), error) {
	name := NetworkPrefix + runID.String()

	created, err := r.cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
		// No gateway, no route out, no NAT. Everything else in this ticket is a
		// second line of defence behind this one flag.
		Internal: true,
		Labels:   map[string]string{RunIDLabel: runID.String()},
	})
	if err != nil {
		return "", func() {}, fmt.Errorf("create the run network: %w", err)
	}

	id := created.ID
	return name, func() {
		// Without the parent's cancellation: a cancelled run is exactly when a
		// network must still be removed.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()

		_ = r.cli.NetworkRemove(cleanup, id)
	}, nil
}

// startProxy puts the run's only way out on its network.
//
// It is attached twice: to the run network, where the generator reaches it by
// name, and to Docker's default bridge, which is where its own packets leave
// from. That second attachment is what the generator does not have.
func (r *Runner) startProxy(ctx context.Context, job Job, networkName string) (string, error) {
	if err := r.ensureProxyImage(ctx); err != nil {
		return "", err
	}

	env := []string{
		"BALLAST_EGRESS_LISTEN=:" + proxyPort,
		"BALLAST_EGRESS_ALLOWED=" + strings.Join(job.AllowedHosts, ","),
	}
	if cidrs := r.allowedCIDRs(ctx, networkName); len(cidrs) > 0 {
		env = append(env, "BALLAST_EGRESS_ALLOWED_CIDRS="+strings.Join(cidrs, ","))
	}

	created, err := r.cli.ContainerCreate(ctx,
		&container.Config{
			Image:  r.cfg.ProxyImage,
			Env:    env,
			User:   containerUser,
			Labels: map[string]string{RunIDLabel: job.RunID.String()},
		},
		&container.HostConfig{
			// It holds no secret and runs no uploaded code, but it is reachable
			// by something that does.
			ReadonlyRootfs: true,
			CapDrop:        []string{"ALL"},
			SecurityOpt:    []string{"no-new-privileges"},
			ExtraHosts:     r.cfg.ExtraHosts,
			AutoRemove:     false,
		},
		&network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
			networkName: {Aliases: []string{proxyAlias}},
		}},
		nil, "",
	)
	if err != nil {
		return "", fmt.Errorf("create the egress proxy: %w", err)
	}

	// The second attachment, and the one the generator will never have: a
	// network with a route out.
	if err := r.cli.NetworkConnect(ctx, "bridge", created.ID, nil); err != nil {
		r.remove(ctx, created.ID)
		return "", fmt.Errorf("give the egress proxy a way out: %w", err)
	}

	if err := r.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		r.remove(ctx, created.ID)
		return "", fmt.Errorf("start the egress proxy: %w", err)
	}

	return created.ID, nil
}

// allowedCIDRs is what the proxy may reach besides the public internet: the
// run network's own subnet, and whatever the configuration named.
//
// The subnet is included because it costs nothing and makes the tests
// honest: a target container on the run network has an address inside it, so the
// allowed path can be exercised without any exception being configured. In
// production no allowed host resolves there, and the host allowlist is checked
// first in any case, so it opens nothing.
func (r *Runner) allowedCIDRs(ctx context.Context, networkName string) []string {
	cidrs := append([]string{}, r.cfg.AllowedCIDRs...)

	inspected, err := r.cli.NetworkInspect(ctx, networkName, network.InspectOptions{})
	if err != nil {
		return cidrs
	}
	for _, config := range inspected.IPAM.Config {
		if config.Subnet != "" {
			cidrs = append(cidrs, config.Subnet)
		}
	}

	return cidrs
}

// ensureProxyImage checks that the proxy is on the machine.
//
// It is never pulled: it is this repository's own code, and there is no registry
// to fetch it from. A machine that has not built it cannot run anything, which
// is the correct failure — a run with no proxy is a run with no way out.
func (r *Runner) ensureProxyImage(ctx context.Context) error {
	if _, _, err := r.cli.ImageInspectWithRaw(ctx, r.cfg.ProxyImage); err != nil {
		if client.IsErrNotFound(err) {
			return fmt.Errorf("%w: the egress proxy image %s is not on this machine; build it with `make proxy-image`",
				ErrNoProxyImage, r.cfg.ProxyImage)
		}
		return fmt.Errorf("look for the egress proxy image: %w", err)
	}
	return nil
}

// ErrNoProxyImage reports a machine that cannot run anything because the one way
// out of a run's network has not been built.
var ErrNoProxyImage = errors.New("hosted: the egress proxy image is missing")

// proxyEnv is what points the generator at the proxy.
//
// Both spellings, because that is how the convention goes, and k6 reads them —
// verified on a real container rather than assumed. NO_PROXY is set to nothing
// so that no host is exempt by accident.
func proxyEnv() map[string]string {
	url := "http://" + proxyAlias + ":" + proxyPort

	return map[string]string{
		"HTTP_PROXY":  url,
		"HTTPS_PROXY": url,
		"http_proxy":  url,
		"https_proxy": url,
		"NO_PROXY":    "",
		"no_proxy":    "",
	}
}
