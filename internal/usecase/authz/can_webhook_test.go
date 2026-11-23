package authz

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// A webhook is not a person or a token either, and Can is where that is decided. The
// rules are the token's, with one fewer permission:
//
//   - the answer comes from the webhook's row, read on every call, so deleting a
//     webhook takes effect on the next request;
//   - a webhook may start a run in its own project, and do nothing else anywhere;
//   - anything unclear fails closed: a service built without webhooks, an identity that
//     is two things, a row that cannot be found.
//
// Written before the code, which is why they do not compile yet.

type fakeWebhooks struct {
	byID  map[uuid.UUID]domain.Webhook
	err   error
	reads int
}

func (f *fakeWebhooks) ByID(_ context.Context, org, id uuid.UUID) (domain.Webhook, bool, error) {
	f.reads++
	if f.err != nil {
		return domain.Webhook{}, false, f.err
	}
	hook, ok := f.byID[id]
	if ok && hook.OrgID != org {
		return domain.Webhook{}, false, nil
	}
	return hook, ok, nil
}

func aWebhook(project uuid.UUID) (domain.Webhook, domain.Identity) {
	hook := domain.Webhook{ID: uuid.New(), OrgID: uuid.New(), ProjectID: project}
	return hook, domain.Identity{OrgID: hook.OrgID, WebhookID: hook.ID}
}

func serviceWithWebhooks(hooks *fakeWebhooks) *Service {
	return newService(fakeMemberships{}).WithWebhooks(hooks)
}

func TestAWebhookMayRunATestInItsProjectAndNothingElse(t *testing.T) {
	project := uuid.New()
	hook, who := aWebhook(project)
	svc := serviceWithWebhooks(&fakeWebhooks{byID: map[uuid.UUID]domain.Webhook{hook.ID: hook}})

	allowed, err := svc.Can(context.Background(), who, domain.PermRunTest, project)
	if err != nil || !allowed {
		t.Fatalf("run: allowed=%v err=%v, want allowed", allowed, err)
	}
	for _, perm := range []domain.Permission{
		domain.PermViewProject, domain.PermManageProject, domain.PermManageScenarios, domain.PermManageEnvironments,
	} {
		if allowed, err := svc.Can(context.Background(), who, perm, project); err != nil || allowed {
			t.Errorf("permission %d: allowed=%v err=%v, want refused", perm, allowed, err)
		}
	}
	other := uuid.New()
	if allowed, _ := svc.Can(context.Background(), who, domain.PermRunTest, other); allowed {
		t.Error("a webhook may run a test in another project")
	}
}

func TestDeletingAWebhookTakesEffectOnTheNextCheck(t *testing.T) {
	project := uuid.New()
	hook, who := aWebhook(project)
	hooks := &fakeWebhooks{byID: map[uuid.UUID]domain.Webhook{hook.ID: hook}}
	svc := serviceWithWebhooks(hooks)

	if allowed, _ := svc.Can(context.Background(), who, domain.PermRunTest, project); !allowed {
		t.Fatal("not allowed before the deletion")
	}
	delete(hooks.byID, hook.ID)
	if allowed, _ := svc.Can(context.Background(), who, domain.PermRunTest, project); allowed {
		t.Error("a deleted webhook is still allowed: its row was not read again")
	}
	if hooks.reads != 2 {
		t.Errorf("the row was read %d times for two checks", hooks.reads)
	}
}

func TestAWebhookOfAnotherOrganizationOrThatIsMissingIsRefused(t *testing.T) {
	project := uuid.New()
	hook, who := aWebhook(project)
	svc := serviceWithWebhooks(&fakeWebhooks{byID: map[uuid.UUID]domain.Webhook{hook.ID: hook}})

	elsewhere := who
	elsewhere.OrgID = uuid.New()
	if allowed, _ := svc.Can(context.Background(), elsewhere, domain.PermRunTest, project); allowed {
		t.Error("a webhook acts in an organization that is not its own")
	}
	missing := domain.Identity{OrgID: who.OrgID, WebhookID: uuid.New()}
	if allowed, _ := svc.Can(context.Background(), missing, domain.PermRunTest, project); allowed {
		t.Error("a webhook that cannot be found is allowed")
	}
}

func TestAFailureToReadAWebhookIsAnErrorNotARefusal(t *testing.T) {
	_, who := aWebhook(uuid.New())
	boom := errors.New("database down")
	svc := serviceWithWebhooks(&fakeWebhooks{err: boom})

	allowed, err := svc.Can(context.Background(), who, domain.PermRunTest, uuid.New())
	if allowed || !errors.Is(err, boom) {
		t.Errorf("allowed=%v err=%v: a database that is down must not read as \"no\"", allowed, err)
	}
}

func TestWithoutAWebhookSourceAWebhookIsRefused(t *testing.T) {
	hook, who := aWebhook(uuid.New())
	_ = hook
	svc := newService(fakeMemberships{})

	if allowed, err := svc.Can(context.Background(), who, domain.PermRunTest, hook.ProjectID); allowed || err != nil {
		t.Errorf("allowed=%v err=%v: an installation wired without webhooks accepted one", allowed, err)
	}
}

func TestAnIdentityThatIsAWebhookAndSomethingElseIsNobody(t *testing.T) {
	project := uuid.New()
	hook, who := aWebhook(project)
	svc := serviceWithWebhooks(&fakeWebhooks{byID: map[uuid.UUID]domain.Webhook{hook.ID: hook}})

	for name, mixed := range map[string]domain.Identity{
		"a person too": {OrgID: who.OrgID, WebhookID: who.WebhookID, UserID: uuid.New()},
		"a token too":  {OrgID: who.OrgID, WebhookID: who.WebhookID, TokenID: uuid.New()},
	} {
		if _, err := svc.Can(context.Background(), mixed, domain.PermRunTest, project); !errors.Is(err, ErrNoActor) {
			t.Errorf("%s: err=%v, want ErrNoActor", name, err)
		}
	}
}

func TestAWebhookIsNeverAllowedInAnyProjectAndIsNotAMember(t *testing.T) {
	project := uuid.New()
	hook, who := aWebhook(project)
	svc := serviceWithWebhooks(&fakeWebhooks{byID: map[uuid.UUID]domain.Webhook{hook.ID: hook}})

	if allowed, err := svc.CanInAnyProject(context.Background(), who, domain.PermRunTest); allowed || err != nil {
		t.Errorf("a webhook is allowed in any project: allowed=%v err=%v", allowed, err)
	}
	if _, found, err := svc.MembershipOf(context.Background(), who, project); found || err != nil {
		t.Errorf("a webhook is a member: found=%v err=%v", found, err)
	}
	mask, err := svc.Permissions(context.Background(), who, project)
	if err != nil || mask != domain.WebhookPermissions() {
		t.Errorf("the mask is %v (err %v), want the webhook's", mask, err)
	}
}

func TestTheMaskOfAWebhookInAnotherProjectIsEmpty(t *testing.T) {
	hook, who := aWebhook(uuid.New())
	svc := serviceWithWebhooks(&fakeWebhooks{byID: map[uuid.UUID]domain.Webhook{hook.ID: hook}})

	mask, err := svc.Permissions(context.Background(), who, uuid.New())
	if err != nil || mask != 0 {
		t.Errorf("the mask is %v (err %v) in a project that is not the webhook's, want none", mask, err)
	}
}
