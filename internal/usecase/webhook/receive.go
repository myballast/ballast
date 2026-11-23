// Package webhook is what a project's signed webhook does: it receives a request, checks
// that whoever sent it holds the project's secret, and asks the run service for a run as
// the webhook; and it lets an administrator make, replace and remove the secret.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Request is a webhook request as it arrived. The headers are text, as they came:
// reading them is part of what is checked.
type Request struct {
	// OrgID and ProjectID are the ones in the address. They name where to look; they
	// prove nothing, and a request that names the wrong ones finds no webhook.
	OrgID, ProjectID uuid.UUID
	Timestamp        string
	Delivery         string
	Signature        string
	Body             []byte
}

// Receiver serves signed requests.
type Receiver struct {
	secrets    Secrets
	deliveries Deliveries
	launcher   Launcher
	clock      Clock
	log        *slog.Logger
}

// NewReceiver wires the receiver.
func NewReceiver(secrets Secrets, deliveries Deliveries, launcher Launcher, clock Clock, log *slog.Logger) *Receiver {
	return &Receiver{secrets: secrets, deliveries: deliveries, launcher: launcher, clock: clock, log: log}
}

// standIn is the secret a request is checked against when the project has no webhook,
// so that "there is none" does the same work as "the signature is wrong". It is not a
// secret: nothing signed with it is accepted, because the answer is refused whatever
// the check says.
const standIn = "blw_a-secret-that-nobody-holds-and-nothing-accepts"

// Receive checks the request and, when it is a good one, starts the run it asks for.
//
// The order is the point. Nothing in the body is read, and nothing is written, until the
// signature has been checked: until then the caller is nobody. The delivery is then
// remembered before the run is asked for, so that two copies of one request that arrive
// together cannot both get through. A request whose run is refused has spent its
// delivery: a sender that tries again signs a new one.
//
// A failure to read the secret or to remember is an error and not a refusal: a database
// that is down must not read as a bad signature, and nothing is started.
func (r *Receiver) Receive(ctx context.Context, req Request) (domain.Run, error) {
	if len(req.Body) > domain.MaxWebhookBody {
		return domain.Run{}, r.refuse(ctx, req, domain.WebhookTooLarge)
	}

	parsed, err := domain.ParseWebhookRequest(req.Timestamp, req.Delivery, req.Signature)
	if err != nil {
		return domain.Run{}, r.refuse(ctx, req, domain.WebhookRefusalReason(err))
	}

	hook, secret, found, err := r.secrets.SecretOf(ctx, req.OrgID, req.ProjectID)
	if err != nil {
		return domain.Run{}, fmt.Errorf("read the webhook's secret: %w", err)
	}
	now := r.clock.Now()
	if !found {
		_ = parsed.Verify(standIn, req.Body, now)
		return domain.Run{}, r.refuse(ctx, req, domain.WebhookUnknown)
	}
	if err := parsed.Verify(secret, req.Body, now); err != nil {
		return domain.Run{}, r.refuse(ctx, req, domain.WebhookRefusalReason(err))
	}

	fresh, err := r.deliveries.Remember(ctx, req.OrgID, req.ProjectID, parsed.DeliveryID, now, now.Add(-domain.WebhookDeliveryRetention))
	if err != nil {
		return domain.Run{}, fmt.Errorf("remember the delivery: %w", err)
	}
	if !fresh {
		return domain.Run{}, r.refuse(ctx, req, domain.WebhookReplayed)
	}

	launch, err := readLaunch(req.Body)
	if err != nil {
		return domain.Run{}, err
	}
	launch.Actor = domain.Identity{OrgID: hook.OrgID, WebhookID: hook.ID}

	return r.launcher.Enqueue(ctx, launch)
}

// refuse is the one answer a sender gets, and the reason that stays in the log. The log
// line carries the reason and where the request was addressed, and nothing the sender
// wrote: not the delivery, not the signature, and certainly not a secret.
func (r *Receiver) refuse(ctx context.Context, req Request, reason domain.WebhookRefusal) error {
	r.log.WarnContext(ctx, "webhook request refused",
		"org_id", req.OrgID, "project_id", req.ProjectID, "reason", string(reason))
	return domain.NewWebhookRefusal(reason)
}

// payload is the body, which is the one of POST /runs.
type payload struct {
	ScenarioID    string `json:"scenario_id"`
	EnvironmentID string `json:"environment_id"`
	Profile       string `json:"profile"`
	Params        *struct {
		TargetVUs int `json:"target_vus"`
		RampUpS   int `json:"ramp_up_s"`
		PlateauS  int `json:"plateau_s"`
		RampDownS int `json:"ramp_down_s"`
	} `json:"params"`
	CI *struct {
		Provider   string `json:"provider"`
		Repository string `json:"repository"`
		Branch     string `json:"branch"`
		Commit     string `json:"commit"`
		JobURL     string `json:"job_url"`
	} `json:"ci"`
}

// readLaunch reads the body as one JSON object and nothing after it. What is wrong is
// said in this code's words: the body is the sender's text, and may hold anything.
func readLaunch(body []byte) (Launch, error) {
	invalid := func(what string) error { return fmt.Errorf("%w: %s", domain.ErrInvalidWebhookBody, what) }

	var p payload
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&p); err != nil {
		return Launch{}, invalid("it is not a JSON object")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Launch{}, invalid("there is something after the JSON object")
	}

	scenario, err := uuid.Parse(p.ScenarioID)
	if err != nil {
		return Launch{}, invalid("scenario_id is not a uuid")
	}
	environment, err := uuid.Parse(p.EnvironmentID)
	if err != nil {
		return Launch{}, invalid("environment_id is not a uuid")
	}

	launch := Launch{ScenarioID: scenario, EnvironmentID: environment, Profile: domain.ProfileKind(p.Profile)}
	if p.Params != nil {
		launch.Params = &domain.ProfileParams{
			TargetVUs: p.Params.TargetVUs, RampUpS: p.Params.RampUpS,
			PlateauS: p.Params.PlateauS, RampDownS: p.Params.RampDownS,
		}
	}
	if p.CI != nil {
		launch.CI = &domain.RunCI{
			Provider: domain.CIProvider(p.CI.Provider), Repository: p.CI.Repository,
			Branch: p.CI.Branch, Commit: p.CI.Commit, JobURL: p.CI.JobURL,
		}
	}
	return launch, nil
}
