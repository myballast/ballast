package webhook_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/webhook"
)

// A signed request starts a run through the same door a person or a token does. What
// these tests hold in place:
//
//   - nothing in the body is read, and nothing is written, until the signature has been
//     checked: the caller is unauthenticated until then, and an unauthenticated caller
//     must not be able to make the server parse, count or remember anything;
//   - every refusal at that stage is the same refusal, and the reason goes to the log
//     only, and without a secret, a signature or an identifier in it;
//   - a delivery is served once. It is remembered when the signature is good, before the
//     run is asked for, so that two copies of one request arriving together cannot both
//     get through;
//   - the run is asked for as the webhook, with the identity that carries no person.
//
// Written before the code, which is why they do not compile yet.

var theMoment = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

const (
	theSecret   = "blw_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"
	theDelivery = "delivery-0123456789"
)

type fakeSecrets struct {
	hook   domain.Webhook
	secret string
	found  bool
	err    error
	asked  [][2]uuid.UUID
}

func (f *fakeSecrets) SecretOf(_ context.Context, org, project uuid.UUID) (domain.Webhook, string, bool, error) {
	f.asked = append(f.asked, [2]uuid.UUID{org, project})
	return f.hook, f.secret, f.found, f.err
}

type fakeDeliveries struct {
	seen         map[string]bool
	err          error
	remembered   []string
	forgetBefore time.Time
}

func (f *fakeDeliveries) Remember(_ context.Context, _, _ uuid.UUID, id string, _, forgetBefore time.Time) (bool, error) {
	f.forgetBefore = forgetBefore
	if f.err != nil {
		return false, f.err
	}
	if f.seen[id] {
		return false, nil
	}
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	f.seen[id] = true
	f.remembered = append(f.remembered, id)
	return true, nil
}

type fakeLauncher struct {
	got   []webhook.Launch
	run   domain.Run
	err   error
	order *[]string
}

func (f *fakeLauncher) Enqueue(_ context.Context, l webhook.Launch) (domain.Run, error) {
	f.got = append(f.got, l)
	if f.order != nil {
		*f.order = append(*f.order, "launch")
	}
	return f.run, f.err
}

type clock struct{ at time.Time }

func (c clock) Now() time.Time { return c.at }

type rig struct {
	receiver   *webhook.Receiver
	secrets    *fakeSecrets
	deliveries *fakeDeliveries
	launcher   *fakeLauncher
	logs       *bytes.Buffer
	org, proj  uuid.UUID
	scenario   uuid.UUID
	env        uuid.UUID
}

func newRig(t *testing.T) *rig {
	t.Helper()

	r := &rig{org: uuid.New(), proj: uuid.New(), scenario: uuid.New(), env: uuid.New(), logs: &bytes.Buffer{}}
	hook := domain.Webhook{ID: uuid.New(), OrgID: r.org, ProjectID: r.proj}
	r.secrets = &fakeSecrets{hook: hook, secret: theSecret, found: true}
	r.deliveries = &fakeDeliveries{}
	r.launcher = &fakeLauncher{run: domain.Run{ID: uuid.New(), Status: domain.StatusQueued}}
	logger := slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r.receiver = webhook.NewReceiver(r.secrets, r.deliveries, r.launcher, clock{at: theMoment}, logger)
	return r
}

func (r *rig) body() string {
	return fmt.Sprintf(`{"scenario_id":%q,"environment_id":%q,"profile":"smoke"}`, r.scenario, r.env)
}

// request signs body as a sender would, at the moment given.
func (r *rig) request(body string, at time.Time, delivery, secret string) webhook.Request {
	stamp := at.Unix()
	return webhook.Request{
		OrgID: r.org, ProjectID: r.proj,
		Timestamp: fmt.Sprint(stamp), Delivery: delivery,
		Signature: domain.SignWebhook(secret, stamp, delivery, []byte(body)),
		Body:      []byte(body),
	}
}

func (r *rig) good() webhook.Request { return r.request(r.body(), theMoment, theDelivery, theSecret) }

func refusedFor(t *testing.T, err error, reason domain.WebhookRefusal) {
	t.Helper()
	if !errors.Is(err, domain.ErrWebhookRefused) {
		t.Fatalf("got %v, want a refusal", err)
	}
	if got := domain.WebhookRefusalReason(err); got != reason {
		t.Errorf("the reason is %q, want %q", got, reason)
	}
}

// --- The way through ------------------------------------------------------------------------

func TestASignedRequestStartsARunAsTheWebhook(t *testing.T) {
	r := newRig(t)

	queued, err := r.receiver.Receive(context.Background(), r.good())
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}

	if queued.ID != r.launcher.run.ID {
		t.Errorf("returned run %s, want the one that was queued", queued.ID)
	}
	if len(r.launcher.got) != 1 {
		t.Fatalf("the run was asked for %d times, want once", len(r.launcher.got))
	}
	got := r.launcher.got[0]
	want := domain.Identity{OrgID: r.org, WebhookID: r.secrets.hook.ID}
	if got.Actor != want {
		t.Errorf("asked as %+v, want %+v: the webhook, and no person or token", got.Actor, want)
	}
	if got.ScenarioID != r.scenario || got.EnvironmentID != r.env || got.Profile != domain.ProfileSmoke {
		t.Errorf("asked for %+v", got)
	}
	if got.Params != nil || got.CI != nil {
		t.Errorf("params %v and ci %v were invented", got.Params, got.CI)
	}
	if len(r.secrets.asked) != 1 || r.secrets.asked[0] != [2]uuid.UUID{r.org, r.proj} {
		t.Errorf("the secret was read for %v", r.secrets.asked)
	}
}

func TestTheParamsAndWhatThePipelineSaysAboutItselfAreCarried(t *testing.T) {
	r := newRig(t)
	body := fmt.Sprintf(`{"scenario_id":%q,"environment_id":%q,"profile":"load",
		"params":{"target_vus":20,"ramp_up_s":5,"plateau_s":30,"ramp_down_s":5},
		"ci":{"provider":"other","repository":"acme/shop","branch":"main"}}`, r.scenario, r.env)

	if _, err := r.receiver.Receive(context.Background(), r.request(body, theMoment, theDelivery, theSecret)); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	got := r.launcher.got[0]
	if got.Params == nil || got.Params.TargetVUs != 20 || got.Params.PlateauS != 30 {
		t.Errorf("params %+v", got.Params)
	}
	if got.CI == nil || got.CI.Provider != domain.CIProvider("other") || got.CI.Repository != "acme/shop" {
		t.Errorf("ci %+v", got.CI)
	}
}

func TestADeliveryIsRememberedBeforeTheRunIsAskedFor(t *testing.T) {
	r := newRig(t)
	var order []string
	r.launcher.order = &order
	r.deliveries.err = nil
	remember := &orderedDeliveries{fakeDeliveries: r.deliveries, order: &order}
	r.receiver = webhook.NewReceiver(r.secrets, remember, r.launcher, clock{at: theMoment}, slog.New(slog.NewTextHandler(r.logs, nil)))

	if _, err := r.receiver.Receive(context.Background(), r.good()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "remember,launch" {
		t.Errorf("order %v: the delivery has to be taken first, or two copies racing can both pass", order)
	}
}

type orderedDeliveries struct {
	*fakeDeliveries
	order *[]string
}

func (o *orderedDeliveries) Remember(ctx context.Context, org, project uuid.UUID, id string, at, before time.Time) (bool, error) {
	*o.order = append(*o.order, "remember")
	return o.fakeDeliveries.Remember(ctx, org, project, id, at, before)
}

func TestDeliveriesPastTheirRetentionAreForgottenWhenOneIsRemembered(t *testing.T) {
	r := newRig(t)
	if _, err := r.receiver.Receive(context.Background(), r.good()); err != nil {
		t.Fatal(err)
	}
	if want := theMoment.Add(-domain.WebhookDeliveryRetention); !r.deliveries.forgetBefore.Equal(want) {
		t.Errorf("forgetting before %s, want %s", r.deliveries.forgetBefore, want)
	}
}

// --- Refusals before anything is read ---------------------------------------------------------

func TestEveryRefusalBeforeTheSignatureHoldsIsTheSameAndStartsNothing(t *testing.T) {
	cases := map[string]struct {
		mutate func(*rig, *webhook.Request)
		reason domain.WebhookRefusal
	}{
		"a wrong secret": {func(r *rig, q *webhook.Request) {
			*q = r.request(r.body(), theMoment, theDelivery, "blw_someone-elses-secret")
		}, domain.WebhookBadSignature},
		"a changed body": {func(_ *rig, q *webhook.Request) { q.Body = append(q.Body, ' ') }, domain.WebhookBadSignature},
		"a stale timestamp": {func(r *rig, q *webhook.Request) {
			*q = r.request(r.body(), theMoment.Add(-10*time.Minute), theDelivery, theSecret)
		}, domain.WebhookStale},
		"a timestamp from the future": {func(r *rig, q *webhook.Request) {
			*q = r.request(r.body(), theMoment.Add(10*time.Minute), theDelivery, theSecret)
		}, domain.WebhookStale},
		"no signature":                {func(_ *rig, q *webhook.Request) { q.Signature = "" }, domain.WebhookMalformed},
		"a short delivery":            {func(_ *rig, q *webhook.Request) { q.Delivery = "abc" }, domain.WebhookMalformed},
		"a timestamp that is not one": {func(_ *rig, q *webhook.Request) { q.Timestamp = "yesterday" }, domain.WebhookMalformed},
		"a body that is too large": {func(r *rig, q *webhook.Request) {
			big := strings.Repeat("a", domain.MaxWebhookBody+1)
			*q = r.request(big, theMoment, theDelivery, theSecret)
		}, domain.WebhookTooLarge},
		"a project with no webhook": {func(r *rig, _ *webhook.Request) { r.secrets.found = false }, domain.WebhookUnknown},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			req := r.good()
			c.mutate(r, &req)

			_, err := r.receiver.Receive(context.Background(), req)

			refusedFor(t, err, c.reason)
			if len(r.launcher.got) != 0 {
				t.Error("a run was asked for")
			}
			if len(r.deliveries.remembered) != 0 {
				t.Error("a delivery was remembered for a request that was not authenticated: anybody could fill the table")
			}
			// The sender is told one sentence whatever failed.
			if err.Error() != domain.ErrWebhookRefused.Error() {
				t.Errorf("the sender is told %q", err.Error())
			}
		})
	}
}

func TestAReplayIsRefusedAndStartsNothing(t *testing.T) {
	r := newRig(t)
	if _, err := r.receiver.Receive(context.Background(), r.good()); err != nil {
		t.Fatalf("the first request: %v", err)
	}

	_, err := r.receiver.Receive(context.Background(), r.good())

	refusedFor(t, err, domain.WebhookReplayed)
	if len(r.launcher.got) != 1 {
		t.Errorf("the run was asked for %d times, want once", len(r.launcher.got))
	}
}

func TestTheSameBytesUnderANewDeliveryAreNotTheSameRequest(t *testing.T) {
	// The delivery is signed, so a captured request cannot be given a new identifier
	// to get past the memory.
	r := newRig(t)
	first := r.good()
	if _, err := r.receiver.Receive(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	forged := first
	forged.Delivery = "delivery-9999999999"
	_, err := r.receiver.Receive(context.Background(), forged)

	refusedFor(t, err, domain.WebhookBadSignature)
}

func TestAnUnknownProjectAndAWrongSignatureCostTheSameWork(t *testing.T) {
	// Not timing, which a unit test cannot see: the shape of the work. A project with
	// no webhook is verified against a stand-in secret all the same, so that "no
	// such webhook" and "wrong signature" are not told apart by one of them returning
	// early.
	r := newRig(t)
	r.secrets.found = false

	_, err := r.receiver.Receive(context.Background(), r.good())

	refusedFor(t, err, domain.WebhookUnknown)
	if len(r.secrets.asked) != 1 {
		t.Errorf("the secret was looked up %d times", len(r.secrets.asked))
	}
}

// --- After the signature --------------------------------------------------------------------

func TestABodyThatIsNotWhatIsExpectedIsRefusedAsARequestAndTheDeliveryIsSpent(t *testing.T) {
	// The signature was good, so the sender is who they say. What is wrong is the
	// request, and they are told so: 400-shaped, with this code's own words.
	// Their delivery is spent: a corrected request is a new delivery.
	for name, body := range map[string]string{
		"not json":                     `not json`,
		"an array":                     `[1,2]`,
		"no scenario":                  `{"environment_id":"c9f0f895-fb98-4b91-99f4-1bf0c1f0d6a1","profile":"smoke"}`,
		"a scenario that is not an id": `{"scenario_id":"x","environment_id":"c9f0f895-fb98-4b91-99f4-1bf0c1f0d6a1","profile":"smoke"}`,
		"no environment":               `{"scenario_id":"8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11","profile":"smoke"}`,
		"two objects":                  `{"scenario_id":"8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11"}{}`,
		"trailing text":                `{"scenario_id":"8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11"} x`,
		"empty":                        ``,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)

			_, err := r.receiver.Receive(context.Background(), r.request(body, theMoment, theDelivery, theSecret))

			if !errors.Is(err, domain.ErrInvalidWebhookBody) {
				t.Fatalf("got %v, want ErrInvalidWebhookBody", err)
			}
			if len(r.launcher.got) != 0 {
				t.Error("a run was asked for")
			}
			if strings.Contains(err.Error(), "not json") {
				t.Errorf("the message repeats the body: %q", err)
			}
		})
	}
}

func TestWhatTheRunServiceRefusesComesBackAsItWasSaid(t *testing.T) {
	for name, refusal := range map[string]error{
		"a target that is not verified": domain.ErrTargetNotVerified,
		"a limit":                       domain.ErrTooManyRuns,
		"the switch":                    domain.ErrRunsSuspended,
		"a window":                      domain.ErrOutsideAllowedWindow,
		"no permission":                 domain.ErrForbidden,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.launcher.err = refusal

			_, err := r.receiver.Receive(context.Background(), r.good())

			if !errors.Is(err, refusal) || errors.Is(err, domain.ErrWebhookRefused) {
				t.Errorf("got %v: a refusal of the run is not a refusal of the signature, and the sender needs to tell them apart", err)
			}
		})
	}
}

// --- Failing closed ----------------------------------------------------------------------------

func TestAFailureToReadTheSecretOrToRememberIsAnErrorAndStartsNothing(t *testing.T) {
	boom := errors.New("database down")
	for name, set := range map[string]func(*rig){
		"the secret": func(r *rig) { r.secrets.err = boom },
		"the memory": func(r *rig) { r.deliveries.err = boom },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			set(r)

			_, err := r.receiver.Receive(context.Background(), r.good())

			if !errors.Is(err, boom) || errors.Is(err, domain.ErrWebhookRefused) {
				t.Errorf("got %v: a database that is down is not a bad signature", err)
			}
			if len(r.launcher.got) != 0 {
				t.Error("a run was asked for although the request could not be checked")
			}
		})
	}
}

func TestNothingIsLoggedThatCouldBeUsedToForgeARequest(t *testing.T) {
	r := newRig(t)
	bad := r.request(r.body(), theMoment, theDelivery, "blw_someone-elses-secret")
	_, _ = r.receiver.Receive(context.Background(), bad)
	_, _ = r.receiver.Receive(context.Background(), r.good())
	_, _ = r.receiver.Receive(context.Background(), r.good())

	logged := r.logs.String()
	for _, leak := range []string{theSecret, "blw_someone-elses-secret", bad.Signature, r.good().Signature, theDelivery} {
		if strings.Contains(logged, leak) {
			t.Errorf("the log holds %q", leak)
		}
	}
	// What the operator reads: that a request was refused, and why.
	for _, want := range []string{"bad_signature", "replayed"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log does not say %q:\n%s", want, logged)
		}
	}
}

func TestABodyOverTheLimitIsRefusedBeforeAnythingIsRead(t *testing.T) {
	// Not even the database: the size is known from the request itself, and a caller who
	// has not proved anything must not be able to make the server look anything up.
	r := newRig(t)
	big := strings.Repeat("a", domain.MaxWebhookBody+1)

	_, err := r.receiver.Receive(context.Background(), r.request(big, theMoment, theDelivery, theSecret))

	refusedFor(t, err, domain.WebhookTooLarge)
	if len(r.secrets.asked) != 0 {
		t.Errorf("the secret was looked up %d times for a body that is too large", len(r.secrets.asked))
	}
}
