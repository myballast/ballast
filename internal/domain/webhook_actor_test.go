package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// A webhook is the third thing that can be behind an action, after a person and an API
// token. The journal and the run both have to say which one, and what these tests hold
// in place is what holds for the other two: they never travel together, a webhook is
// never written down as nobody, and an entry with no author at all is Ballast acting
// alone.
//
// Written before the code, which is why they do not compile yet.

func TestAWebhookActsAsAWebhook(t *testing.T) {
	who := Identity{WebhookID: uuid.New(), OrgID: uuid.New()}

	actor := who.Actor()

	if actor.WebhookID == nil || *actor.WebhookID != who.WebhookID {
		t.Errorf("the webhook is %v, want %s", actor.WebhookID, who.WebhookID)
	}
	if actor.UserID != nil || actor.TokenID != nil {
		t.Errorf("a webhook acts as %+v", actor)
	}
	if actor.IsZero() {
		t.Error("a webhook is nobody: its actions would be written as Ballast's own")
	}
	if !who.IsWebhook() || who.IsToken() {
		t.Error("the identity does not say it is a webhook, and only that")
	}
}

func TestAnIdentityThatIsTwoThingsOrNoneIsNobody(t *testing.T) {
	org := uuid.New()
	for name, who := range map[string]Identity{
		"a person and a webhook": {UserID: uuid.New(), WebhookID: uuid.New(), OrgID: org},
		"a token and a webhook":  {TokenID: uuid.New(), WebhookID: uuid.New(), OrgID: org},
		"all three":              {UserID: uuid.New(), TokenID: uuid.New(), WebhookID: uuid.New(), OrgID: org},
		"neither":                {OrgID: org},
	} {
		if actor := who.Actor(); !actor.IsZero() {
			t.Errorf("%s: acts as %+v, want nobody", name, actor)
		}
	}
}

func TestARunIsLaunchedByOneKindOfActorAtATime(t *testing.T) {
	person, token, webhook := uuid.New(), uuid.New(), uuid.New()
	run := Run{TriggeredBy: &person, TriggeredByToken: &token}

	run = run.By(Actor{WebhookID: &webhook})

	if run.TriggeredByWebhook == nil || *run.TriggeredByWebhook != webhook {
		t.Errorf("the webhook is %v, want %s", run.TriggeredByWebhook, webhook)
	}
	if run.TriggeredBy != nil || run.TriggeredByToken != nil {
		t.Errorf("the run still names %v and %v: By replaces, it does not add", run.TriggeredBy, run.TriggeredByToken)
	}
	if run.By(Actor{UserID: &person}).TriggeredByWebhook != nil {
		t.Error("a run launched by a person still names a webhook")
	}
}

func TestAnEntryIsByOneKindOfActorAtATime(t *testing.T) {
	person, token, webhook := uuid.New(), uuid.New(), uuid.New()
	entry := entryAt().By(Actor{UserID: &person}).By(Actor{TokenID: &token})
	entry = entry.By(Actor{WebhookID: &webhook})

	if entry.ActorWebhookID == nil || *entry.ActorWebhookID != webhook || entry.ActorID != nil || entry.ActorTokenID != nil {
		t.Errorf("the entry names %v, %v and %v", entry.ActorID, entry.ActorTokenID, entry.ActorWebhookID)
	}
}

// --- What a webhook may do -----------------------------------------------------------------

func TestAWebhookMayStartARunInItsProjectAndNothingElse(t *testing.T) {
	project := uuid.New()
	hook := Webhook{ID: uuid.New(), OrgID: uuid.New(), ProjectID: project}

	if !hook.Allows(PermRunTest, project) {
		t.Error("a webhook may not start a run in its own project")
	}
	if hook.Allows(PermRunTest, uuid.New()) {
		t.Error("a webhook may start a run in another project")
	}
	if hook.Allows(PermRunTest, uuid.Nil) {
		t.Error("a webhook may start a run in no project")
	}
	// Not even to read: a webhook has nobody to show anything to, and a permission it
	// does not need is one it must not hold.
	for _, perm := range []Permission{PermViewProject, PermManageProject, PermManageScenarios, PermManageEnvironments, 0} {
		if hook.Allows(perm, project) {
			t.Errorf("a webhook holds %d", perm)
		}
	}
	// Every permission asked has to be held: "run and manage" is not answered by "run".
	if hook.Allows(PermRunTest|PermManageProject, project) {
		t.Error("a request for two permissions was answered by one of them")
	}
	if WebhookPermissions() != PermissionSet(PermRunTest) {
		t.Errorf("the webhook's permissions are %v, want only running a test", WebhookPermissions())
	}
}

// --- The journal -----------------------------------------------------------------------------

func TestTheEntriesOfAWebhookCarryAClosedSetOfKeysAndNeverTheSecret(t *testing.T) {
	person := uuid.New()
	hook := Webhook{ID: uuid.New(), OrgID: uuid.New(), ProjectID: uuid.New()}
	at := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	for name, entry := range map[string]AuditEntry{
		"created": WebhookCreatedEntry(&person, hook, at),
		"rotated": WebhookRotatedEntry(&person, hook, at),
		"deleted": WebhookDeletedEntry(&person, hook, at),
	} {
		if entry.ProjectID == nil || *entry.ProjectID != hook.ProjectID || entry.TargetID == nil || *entry.TargetID != hook.ID {
			t.Errorf("%s: the entry is not about this webhook of this project", name)
		}
		if entry.TargetType != TargetWebhook || TargetWebhook != "webhook" {
			t.Errorf("%s: target %q", name, entry.TargetType)
		}
		if entry.ActorID == nil || *entry.ActorID != person {
			t.Errorf("%s: the person who acted is not named", name)
		}
		if len(entry.Details) != 0 {
			t.Errorf("%s: carries %v: there is nothing about a webhook that is not already its row", name, entry.Details)
		}
	}
	if WebhookCreatedEntry(&person, hook, at).Action != ActionWebhookCreated ||
		WebhookRotatedEntry(&person, hook, at).Action != ActionWebhookRotated ||
		WebhookDeletedEntry(&person, hook, at).Action != ActionWebhookDeleted {
		t.Error("an entry has the wrong action")
	}
}
