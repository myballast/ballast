package main

import (
	"context"

	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
	"github.com/youssefmahersi/ballast/internal/usecase/webhook"
)

// runLauncher lets a project's webhook ask the run service for a run. The webhook has no
// path of its own to a run: it is handed the one a person and a token use, and so meets
// every guardrail in the same place. Written here, where the layers are assembled,
// because the webhook package declares what it needs and does not import the run
// package.
type runLauncher struct{ runs *run.Service }

func (l runLauncher) Enqueue(ctx context.Context, launch webhook.Launch) (domain.Run, error) {
	return l.runs.Enqueue(ctx, run.EnqueueCommand{
		Actor:         launch.Actor,
		ScenarioID:    launch.ScenarioID,
		EnvironmentID: launch.EnvironmentID,
		Profile:       launch.Profile,
		Params:        launch.Params,
		CI:            launch.CI,
	})
}
