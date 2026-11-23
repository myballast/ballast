// Command ballast starts a load test run on Ballast from a pipeline, follows it, and
// leaves with a code the pipeline can act on: 0 when its thresholds held, 1 when one
// was breached, 2 for anything else.
//
// It is the common base of the GitHub Action and the GitLab template, and it is the
// one piece of Ballast that runs on somebody else's machine. It imports nothing from
// the server's adapters; everything it does is in internal/cli, and a test keeps it
// so.
//
//	BALLAST_URL=https://ballast.example.com BALLAST_TOKEN=blt_... \
//	  ballast run --scenario ID --environment ID --profile smoke
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/youssefmahersi/ballast/internal/cli"
)

func main() {
	ctx, firstSignal := context.WithCancel(context.Background())
	secondSignal := make(chan struct{})

	signals := make(chan os.Signal, 3)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	go func() {
		// The first asks the run to stop before leaving. The second is believed and
		// leaves without waiting for the answer. A third leaves now, whatever the tool
		// was doing: a person who has pressed Ctrl+C three times is not to be argued
		// with.
		<-signals
		firstSignal()
		<-signals
		close(secondSignal)
		<-signals
		os.Exit(cli.ExitError)
	}()

	os.Exit(cli.Main(ctx, secondSignal, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}
