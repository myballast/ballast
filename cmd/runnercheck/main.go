// Command runnercheck says whether a runner is what it should be, as the Runners
// screen would say it.
//
// It exists for the deployment tests (ops/tests/runner-test.sh), which have to know
// whether a worker is online, draining or offline *as Ballast sees it* — which is
// to say from the database, with the same rule the screen applies — and not merely
// whether its container is up. It is built into the worker's image so that it
// reads the database with the worker's own configuration and nothing else.
//
//	runnercheck -expect online
//
// Exit 0 when the runner is what was expected, 1 when it is not, 2 when it could
// not be checked at all. What it saw is printed either way: no secret, no URL.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra/config"
)

const absent = "absent"

func main() {
	os.Exit(run())
}

func run() int {
	expect := flag.String("expect", "online", "online, draining, offline or absent")
	name := flag.String("name", "", "the runner's name; defaults to the worker's own (BALLAST_RUNNER_NAME)")
	flag.Parse()

	want, err := parseExpectation(*expect)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	cfg, err := config.LoadWorker()
	if err != nil {
		fmt.Fprintln(os.Stderr, "the worker's configuration is not readable:", err)
		return 2
	}
	if *name == "" {
		*name = cfg.RunnerName
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		// Said without the error: a driver's message can carry the address and the
		// user it was given.
		fmt.Fprintln(os.Stderr, "the database could not be reached")
		return 2
	}
	defer db.Close()

	runners, err := postgres.NewRunnerRepository(db).List(ctx, cfg.OrgID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "the runners could not be read")
		return 2
	}

	ok, saw := check(runners, *name, time.Now(), want)
	fmt.Println(saw)
	if !ok {
		return 1
	}
	return 0
}

// parseExpectation refuses a word that is not one. A misspelt expectation must
// read neither as a failure of the runner nor as a pass.
func parseExpectation(raw string) (string, error) {
	switch raw {
	case string(domain.RunnerOnline), string(domain.RunnerStatusDraining), string(domain.RunnerOffline), absent:
		return raw, nil
	default:
		return "", fmt.Errorf("-expect is %q; it is one of online, draining, offline, absent", raw)
	}
}

// check says whether the named runner is what was expected at now, and what was
// seen. Status is the domain's own, read the way the screen reads it: never
// stored, worked out from how long ago the runner last spoke.
func check(runners []domain.Runner, name string, now time.Time, expect string) (bool, string) {
	for _, runner := range runners {
		if runner.Name != name {
			continue
		}

		status := runner.StatusAt(now)
		saw := fmt.Sprintf("%s is %s (said %q %s ago, version %s, contract %d)",
			name, status, runner.State, now.Sub(runner.HeartbeatAt).Round(time.Second),
			runner.Version, runner.Contract)
		return string(status) == expect, saw
	}

	return expect == absent, fmt.Sprintf("no runner named %s has reported", name)
}
