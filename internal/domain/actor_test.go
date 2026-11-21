package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Who is behind an action is a person or an API token, and the journal and the
// run both have to say which. What these tests hold in place: the two never
// travel together, a token is never written down as nobody, and Ballast acting
// alone is still the one entry with no author at all.
//
// Written before the code, which is why they do not compile yet.

func TestAPersonActsAsAPerson(t *testing.T) {
	who := Identity{UserID: uuid.New(), OrgID: uuid.New()}

	actor := who.Actor()

	if actor.UserID == nil || *actor.UserID != who.UserID {
		t.Errorf("the person is %v, want %s", actor.UserID, who.UserID)
	}
	if actor.TokenID != nil {
		t.Errorf("a person acts with the token %v", actor.TokenID)
	}
	if actor.IsZero() {
		t.Error("a person is nobody")
	}
}

func TestATokenActsAsAToken(t *testing.T) {
	who := Identity{TokenID: uuid.New(), OrgID: uuid.New()}

	actor := who.Actor()

	if actor.TokenID == nil || *actor.TokenID != who.TokenID {
		t.Errorf("the token is %v, want %s", actor.TokenID, who.TokenID)
	}
	if actor.UserID != nil {
		t.Errorf("a token acts as the person %v", actor.UserID)
	}
	if actor.IsZero() {
		t.Error("a token is nobody: its actions would be written as Ballast's own")
	}
}

// An identity that is both, or neither, is not anybody. Resolving it to whichever
// is read first would be a guess, and the journal is where a guess does harm.
func TestAnIdentityThatIsBothOrNeitherIsNobody(t *testing.T) {
	both := Identity{UserID: uuid.New(), TokenID: uuid.New(), OrgID: uuid.New()}
	neither := Identity{OrgID: uuid.New()}

	for name, who := range map[string]Identity{"both": both, "neither": neither} {
		if actor := who.Actor(); !actor.IsZero() {
			t.Errorf("%s: acts as %+v, want nobody", name, actor)
		}
	}
}

// What comes back does not point into the identity it was made from: a copy
// kept in a run or an entry must not change when the identity does.
func TestAnActorIsACopyOfTheIdentity(t *testing.T) {
	who := Identity{UserID: uuid.New(), OrgID: uuid.New()}
	actor := who.Actor()
	before := *actor.UserID

	who.UserID = uuid.New()

	if *actor.UserID != before {
		t.Error("the actor followed a change made to the identity")
	}
}

// --- The journal ---------------------------------------------------------------

func entryAt() AuditEntry {
	return Allow(uuid.New(), nil, ActionRunQueued, TargetRun, time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC))
}

func TestAnEntryDoneByAPersonNamesThePersonAndNoToken(t *testing.T) {
	who := Identity{UserID: uuid.New(), OrgID: uuid.New()}

	entry := entryAt().By(who.Actor())

	if entry.ActorID == nil || *entry.ActorID != who.UserID {
		t.Errorf("the entry names %v, want %s", entry.ActorID, who.UserID)
	}
	if entry.ActorTokenID != nil {
		t.Errorf("an entry done by a person names the token %v", entry.ActorTokenID)
	}
}

// A run launched by a pipeline is somebody's doing, and the somebody is the
// token. An entry that left it blank would read as Ballast acting on nobody's
// behalf — the one thing it must not say about a credential somebody holds.
func TestAnEntryDoneByATokenNamesTheTokenAndNoPerson(t *testing.T) {
	who := Identity{TokenID: uuid.New(), OrgID: uuid.New()}

	entry := entryAt().By(who.Actor())

	if entry.ActorTokenID == nil || *entry.ActorTokenID != who.TokenID {
		t.Errorf("the entry names %v, want the token %s", entry.ActorTokenID, who.TokenID)
	}
	if entry.ActorID != nil {
		t.Errorf("an entry done by a token names the person %v", entry.ActorID)
	}
}

// The one entry with no author at all is Ballast acting alone, and By is what
// says so: it replaces whoever was there, it does not add to them.
func TestAnEntryDoneByNobodyHasNoAuthorOfEitherKind(t *testing.T) {
	person, token := uuid.New(), uuid.New()
	entry := entryAt()
	entry.ActorID, entry.ActorTokenID = &person, &token

	entry = entry.By(Actor{})

	if entry.ActorID != nil || entry.ActorTokenID != nil {
		t.Errorf("an entry done by nobody names %v and %v", entry.ActorID, entry.ActorTokenID)
	}
}

func TestChangingWhoDidItDoesNotEditAnotherEntry(t *testing.T) {
	original := entryAt()
	other := original.By(Identity{TokenID: uuid.New(), OrgID: uuid.New()}.Actor())

	if original.ActorTokenID != nil {
		t.Error("By edited the entry it was called on")
	}
	if other.ActorTokenID == nil {
		t.Error("By returned an entry with no token")
	}
}

// The builders that exist already take a person, or nobody. Neither can produce
// a token's entry by accident.
func TestTheEntriesThatExistNameNoToken(t *testing.T) {
	person := uuid.New()

	for name, entry := range map[string]AuditEntry{
		"allowed, by a person": Allow(uuid.New(), &person, ActionRunQueued, TargetRun, time.Now()),
		"allowed, by nobody":   Allow(uuid.New(), nil, ActionRunQueued, TargetRun, time.Now()),
		"refused, by a person": Refuse(uuid.New(), &person, ActionRunRefused, TargetRun, "reason", time.Now()),
		"refused, by nobody":   Refuse(uuid.New(), nil, ActionRunRefused, TargetRun, "reason", time.Now()),
	} {
		if entry.ActorTokenID != nil {
			t.Errorf("%s: names the token %v", name, entry.ActorTokenID)
		}
	}
}

// --- The run -------------------------------------------------------------------

func aRun(triggeredBy *uuid.UUID) Run {
	return NewRun(uuid.New(), Scenario{ID: uuid.New(), ProjectID: uuid.New(), Version: 3},
		Environment{ID: uuid.New()}, ProfileSmoke, ProfileParams{}, triggeredBy)
}

func TestARunLaunchedByAPersonRemembersThePerson(t *testing.T) {
	who := Identity{UserID: uuid.New(), OrgID: uuid.New()}

	run := aRun(nil).By(who.Actor())

	if run.TriggeredBy == nil || *run.TriggeredBy != who.UserID {
		t.Errorf("triggered by %v, want %s", run.TriggeredBy, who.UserID)
	}
	if run.TriggeredByToken != nil {
		t.Errorf("a run launched by a person remembers the token %v", run.TriggeredByToken)
	}
}

// A run launched by a pipeline has no person, and says which token it was. That
// is what tells it from the run of somebody who has since left, whose person was
// cleared by the foreign key and who is nobody and no token.
func TestARunLaunchedByATokenRemembersTheTokenAndNoPerson(t *testing.T) {
	who := Identity{TokenID: uuid.New(), OrgID: uuid.New()}

	run := aRun(nil).By(who.Actor())

	if run.TriggeredByToken == nil || *run.TriggeredByToken != who.TokenID {
		t.Errorf("triggered by token %v, want %s", run.TriggeredByToken, who.TokenID)
	}
	if run.TriggeredBy != nil {
		t.Errorf("a run launched by a token remembers the person %v", run.TriggeredBy)
	}
}

func TestARunNeverRemembersBothAPersonAndAToken(t *testing.T) {
	person := uuid.New()
	token := Identity{TokenID: uuid.New(), OrgID: uuid.New()}.Actor()

	// Started as a person's, then said to be a token's: the token replaces.
	run := aRun(&person).By(token)

	if run.TriggeredBy != nil || run.TriggeredByToken == nil {
		t.Errorf("person %v and token %v: want the token alone", run.TriggeredBy, run.TriggeredByToken)
	}
}

func TestARunWithNoOneBehindItRemembersNoOne(t *testing.T) {
	person := uuid.New()

	run := aRun(&person).By(Actor{})

	if run.TriggeredBy != nil || run.TriggeredByToken != nil {
		t.Errorf("a run done by nobody remembers %v and %v", run.TriggeredBy, run.TriggeredByToken)
	}
}

func TestARunBuiltTheOldWayStillWorks(t *testing.T) {
	person := uuid.New()

	run := aRun(&person)

	if run.TriggeredBy == nil || *run.TriggeredBy != person || run.TriggeredByToken != nil {
		t.Errorf("triggered by %v / %v, want the person", run.TriggeredBy, run.TriggeredByToken)
	}
	if run.Status != StatusQueued {
		t.Errorf("status %q, want queued", run.Status)
	}
}
