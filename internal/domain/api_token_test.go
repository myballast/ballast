package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/google/uuid"
)

// An API token is what a pipeline holds instead of a session. It is a bearer
// credential that ends up in a CI secret store, so what these tests defend is
// what a stolen copy of the database, a log line or a screenshot of the list
// would give away: nothing that works.
//
// Written before the code, which is why they do not compile yet.

// fixedBytes is a random source that always gives the same bytes, so a secret
// can be compared across calls without depending on the operating system.
func fixedBytes(b byte) *bytes.Reader {
	return bytes.NewReader(bytes.Repeat([]byte{b}, 64))
}

func TestANewSecretCarriesTheDetectablePrefixAndALotOfRandomness(t *testing.T) {
	secret, err := NewAPITokenSecret(fixedBytes(0x41))
	if err != nil {
		t.Fatalf("NewAPITokenSecret: %v", err)
	}

	// A readable prefix is what lets a secret scanner, and a person reading a
	// leaked log, recognise the string for what it is.
	if !strings.HasPrefix(secret, APITokenPrefix) || APITokenPrefix != "blt_" {
		t.Errorf("secret %q does not start with the prefix blt_", secret)
	}
	// 32 random bytes in base64url without padding is 43 characters. Fewer
	// bytes would be a guessable credential.
	if got, want := len(secret), len(APITokenPrefix)+43; got != want {
		t.Errorf("secret is %d characters, want %d: 32 random bytes", got, want)
	}
	// The alphabet is URL-safe: the secret travels in headers and shell
	// variables, and must survive both without quoting.
	for _, c := range strings.TrimPrefix(secret, APITokenPrefix) {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
		if !ok {
			t.Fatalf("secret %q holds %q, which is not URL-safe", secret, c)
		}
	}
}

func TestTwoSecretsDiffer(t *testing.T) {
	a, err := NewAPITokenSecret(bytes.NewReader(bytes.Repeat([]byte{1}, 64)))
	if err != nil {
		t.Fatalf("NewAPITokenSecret: %v", err)
	}
	b, err := NewAPITokenSecret(bytes.NewReader(bytes.Repeat([]byte{2}, 64)))
	if err != nil {
		t.Fatalf("NewAPITokenSecret: %v", err)
	}
	if a == b {
		t.Error("two different random inputs produced the same secret")
	}
}

// A predictable credential proves nothing about who holds it. If the system's
// randomness cannot be read, there is no token, and never a weaker one.
func TestASecretIsRefusedWhenRandomnessFails(t *testing.T) {
	failing := iotest.ErrReader(errors.New("entropy exhausted"))
	if secret, err := NewAPITokenSecret(failing); err == nil || secret != "" {
		t.Errorf("a secret was made from a failing source: %q, %v", secret, err)
	}

	// Fewer bytes than asked for is a short read, not a shorter token.
	short := bytes.NewReader([]byte{1, 2, 3})
	if secret, err := NewAPITokenSecret(short); err == nil || secret != "" {
		t.Errorf("a secret was made from 3 random bytes: %q, %v", secret, err)
	}
}

// Only the hash is ever stored. It is the plain SHA-256: the secret already has
// 256 bits of entropy, so there is nothing to slow down, and a lookup by hash
// has to be exact and cheap.
func TestTheHashIsTheSHA256OfTheWholeSecret(t *testing.T) {
	secret, err := NewAPITokenSecret(fixedBytes(0x07))
	if err != nil {
		t.Fatalf("NewAPITokenSecret: %v", err)
	}

	sum := sha256.Sum256([]byte(secret))
	if got, want := HashAPITokenSecret(secret), hex.EncodeToString(sum[:]); got != want {
		t.Errorf("hash %q, want %q", got, want)
	}
	first, second := HashAPITokenSecret(secret), HashAPITokenSecret(secret)
	if first != second {
		t.Error("hashing is not deterministic")
	}
	if HashAPITokenSecret(secret) == HashAPITokenSecret(secret+"x") {
		t.Error("two different secrets share a hash")
	}
}

// The prefix is what the list shows so that somebody can tell two tokens apart.
// It must not be enough to rebuild the secret, or to narrow a search for it.
func TestTheDisplayPrefixRevealsAlmostNothing(t *testing.T) {
	secret, err := NewAPITokenSecret(fixedBytes(0x55))
	if err != nil {
		t.Fatalf("NewAPITokenSecret: %v", err)
	}

	prefix := APITokenDisplayPrefix(secret)

	if !strings.HasPrefix(secret, prefix) {
		t.Errorf("prefix %q is not the start of the secret", prefix)
	}
	// "blt_" plus 8 characters: 48 bits of the 256, which leaves nothing to
	// guess from and still tells two tokens apart.
	if got, want := len(prefix), len(APITokenPrefix)+8; got != want {
		t.Errorf("prefix is %d characters, want %d", got, want)
	}
	if prefix == secret {
		t.Error("the display prefix is the whole secret")
	}
	if got := APITokenDisplayPrefix("short"); got != "" {
		t.Errorf("a string that is not a secret has the prefix %q", got)
	}
}

// Whatever arrives in an Authorization header is somebody else's input. The
// shape check keeps garbage away from the database, and a value that is not a
// token never costs a query.
func TestOnlyTheRightShapeLooksLikeAToken(t *testing.T) {
	good, err := NewAPITokenSecret(fixedBytes(0x33))
	if err != nil {
		t.Fatalf("NewAPITokenSecret: %v", err)
	}
	if !LooksLikeAPIToken(good) {
		t.Fatalf("a freshly made secret %q does not look like a token", good)
	}

	for _, bad := range []string{
		"",
		"blt_",
		"blt_short",
		good[:len(good)-1],
		good + "A",
		" " + good,
		good + " ",
		good + "\n",
		strings.Replace(good, "blt_", "xyz_", 1),
		strings.ToUpper(good),
		good[:len(good)-1] + "!",
		good[:len(good)-1] + "%",
		strings.Repeat("A", 100_000),
		"Bearer " + good,
	} {
		if LooksLikeAPIToken(bad) {
			t.Errorf("%.60q looks like a token", bad)
		}
	}
}

func TestANameIsTrimmedAndBounded(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"github main", "github main", true},
		{"  ci  ", "ci", true},
		{"a", "a", true},
		{strings.Repeat("a", 64), strings.Repeat("a", 64), true},
		{"", "", false},
		{"   ", "", false},
		{strings.Repeat("a", 65), "", false},
		// A name is printed in lists and in the audit log. A control character
		// in it is a forged line in somebody's terminal.
		{"line\nbreak", "", false},
		{"tab\there", "", false},
		{"nul\x00byte", "", false},
		{"escape\x1b[31m", "", false},
		{"line\u2028separator", "", false},
		{"é accentué", "é accentué", true},
	}

	for _, c := range cases {
		got, err := ValidateAPITokenName(c.in)
		switch {
		case c.ok && err != nil:
			t.Errorf("%q was refused: %v", c.in, err)
		case !c.ok && !errors.Is(err, ErrInvalidAPIToken):
			t.Errorf("%q: error %v, want ErrInvalidAPIToken", c.in, err)
		case c.ok && got != c.want:
			t.Errorf("%q became %q, want %q", c.in, got, c.want)
		}
	}
}

// A token that never expires is a credential nobody remembers having issued.
// There is always an expiry: the default when none is asked for, and a ceiling
// on what can be asked.
func TestAnExpiryIsAlwaysSetAndBounded(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	got, err := ResolveAPITokenExpiry(nil, now)
	if err != nil {
		t.Fatalf("no expiry asked for: %v", err)
	}
	if want := now.Add(90 * 24 * time.Hour); !got.Equal(want) || APITokenDefaultTTL != 90*24*time.Hour {
		t.Errorf("default expiry %v, want %v (90 days)", got, want)
	}

	within := now.Add(30 * 24 * time.Hour)
	if got, err := ResolveAPITokenExpiry(&within, now); err != nil || !got.Equal(within) {
		t.Errorf("an expiry in 30 days became %v, %v", got, err)
	}

	limit := now.Add(APITokenMaxTTL)
	if _, err := ResolveAPITokenExpiry(&limit, now); err != nil {
		t.Errorf("the longest allowed expiry was refused: %v", err)
	}
	if APITokenMaxTTL != 365*24*time.Hour {
		t.Errorf("the ceiling is %v, want a year", APITokenMaxTTL)
	}

	for name, at := range map[string]time.Time{
		"now":              now,
		"the past":         now.Add(-time.Hour),
		"the zero time":    {},
		"beyond a year":    limit.Add(time.Second),
		"in ten years":     now.Add(10 * 365 * 24 * time.Hour),
		"a second ago":     now.Add(-time.Second),
		"a nanosecond ago": now.Add(-time.Nanosecond),
	} {
		at := at
		if _, err := ResolveAPITokenExpiry(&at, now); !errors.Is(err, ErrInvalidAPIToken) {
			t.Errorf("an expiry at %s: error %v, want ErrInvalidAPIToken", name, err)
		}
	}
}

func tokenAt(project uuid.UUID, now time.Time) APIToken {
	return APIToken{
		ID:        uuid.New(),
		OrgID:     uuid.New(),
		ProjectID: project,
		Name:      "ci",
		ExpiresAt: now.Add(24 * time.Hour),
	}
}

func TestATokenIsActiveExpiredOrRevoked(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	project := uuid.New()

	active := tokenAt(project, now)
	if got := active.State(now); got != APITokenActive {
		t.Errorf("a fresh token is %q", got)
	}

	// The instant of expiry is already too late, like an invitation's: a
	// credential does not work "until" a moment and at it.
	expired := tokenAt(project, now)
	expired.ExpiresAt = now
	if got := expired.State(now); got != APITokenExpired {
		t.Errorf("a token at its expiry is %q, want expired", got)
	}

	revokedAt := now.Add(-time.Hour)
	revoked := tokenAt(project, now)
	revoked.RevokedAt = &revokedAt
	if got := revoked.State(now); got != APITokenRevoked {
		t.Errorf("a revoked token is %q", got)
	}

	// Revoked outranks expired: a token somebody withdrew was withdrawn, whatever
	// its date, and the audit log should say so.
	both := tokenAt(project, now)
	both.ExpiresAt = now.Add(-time.Minute)
	both.RevokedAt = &revokedAt
	if got := both.State(now); got != APITokenRevoked {
		t.Errorf("a token both revoked and expired is %q, want revoked", got)
	}
}

// The permissions of a token are not a role and not a mask read from anywhere:
// they are two fixed ones, in one project. A token that could do more would be
// a way around the roles.
func TestATokenHoldsExactlyTwoPermissions(t *testing.T) {
	want := PermissionSet(PermViewProject | PermRunTest)
	if got := APITokenPermissions(); got != want {
		t.Errorf("a token's permissions are %b, want %b (view and run)", got, want)
	}
}

func TestATokenAllowsOnlyItsOwnPermissionsInItsOwnProject(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	project := uuid.New()
	token := tokenAt(project, now)

	cases := []struct {
		name    string
		perm    Permission
		project uuid.UUID
		want    bool
	}{
		{"view its project", PermViewProject, project, true},
		{"run in its project", PermRunTest, project, true},
		{"cancel a run", PermCancelRun, project, false},
		{"manage scenarios", PermManageScenarios, project, false},
		{"manage environments", PermManageEnvironments, project, false},
		{"approve a target", PermApproveTarget, project, false},
		{"manage members", PermManageMembers, project, false},
		{"manage the project", PermManageProject, project, false},
		{"view another project", PermViewProject, uuid.New(), false},
		{"run in another project", PermRunTest, uuid.New(), false},
		{"run in no project", PermRunTest, uuid.Nil, false},
		// Has() on a set is "any bit". A token must be "all bits": asking for
		// run and manage together must not be answered by the run alone.
		{"run and manage together", PermRunTest | PermManageProject, project, false},
		{"run and view together", PermRunTest | PermViewProject, project, true},
		{"nothing at all", 0, project, false},
	}

	for _, c := range cases {
		if got := token.Allows(c.perm, c.project, now); got != c.want {
			t.Errorf("%s: Allows = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestADeadTokenAllowsNothing(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	project := uuid.New()

	expired := tokenAt(project, now)
	expired.ExpiresAt = now.Add(-time.Second)
	revokedAt := now.Add(-time.Hour)
	revoked := tokenAt(project, now)
	revoked.RevokedAt = &revokedAt

	for name, token := range map[string]APIToken{"expired": expired, "revoked": revoked} {
		for _, perm := range []Permission{PermViewProject, PermRunTest} {
			if token.Allows(perm, project, now) {
				t.Errorf("a %s token allows permission %d", name, perm)
			}
		}
	}
}

// A token has no person. An identity with a token and no user is how every
// layer below tells the two kinds of caller apart, and one with neither is
// nobody.
func TestAnIdentityIsATokenWhenItCarriesOne(t *testing.T) {
	person := Identity{UserID: uuid.New(), OrgID: uuid.New()}
	if person.IsToken() {
		t.Error("a person is a token")
	}

	token := Identity{OrgID: uuid.New(), TokenID: uuid.New()}
	if !token.IsToken() {
		t.Error("an identity with a token id is not a token")
	}
	if token.UserID != uuid.Nil {
		t.Error("a token identity carries a user")
	}
}

// --- The journal and the limit ---------------------------------------------------

// What the journal says about tokens is part of the data: a word renamed makes
// every entry already written unreadable by whoever looks for the new one.
func TestTheWordsTheJournalUsesForTokensAreFrozen(t *testing.T) {
	if ActionAPITokenCreated != "api_token.created" {
		t.Errorf("creating a token is recorded as %q", ActionAPITokenCreated)
	}
	if ActionAPITokenRevoked != "api_token.revoked" {
		t.Errorf("revoking a token is recorded as %q", ActionAPITokenRevoked)
	}
	if TargetAPIToken != "api_token" {
		t.Errorf("the target of those entries is %q", TargetAPIToken)
	}
}

// Without a ceiling, creating tokens is a point of unlimited writing, and a
// project that has twenty live credentials has lost track of them.
func TestAProjectHoldsAtMostTwentyLiveTokens(t *testing.T) {
	if MaxAPITokensPerProject != 20 {
		t.Errorf("the ceiling is %d, want 20", MaxAPITokensPerProject)
	}
	if !errors.Is(ErrTooManyAPITokens, ErrTooManyAPITokens) || errors.Is(ErrTooManyAPITokens, ErrInvalidAPIToken) {
		t.Error("the ceiling must be its own error: it is a conflict with what exists, not a mistake in the request")
	}
}

func journaled() (APIToken, uuid.UUID, time.Time) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	token := APIToken{
		ID:        uuid.New(),
		OrgID:     uuid.New(),
		ProjectID: uuid.New(),
		// A name a person chose, and so free text: not for the journal.
		Name:      "deploy key of the payments team",
		Prefix:    "blt_Ab3dEf9h",
		ExpiresAt: now.Add(90 * 24 * time.Hour),
	}
	return token, uuid.New(), now
}

func keysOf(details map[string]string) []string {
	keys := make([]string, 0, len(details))
	for k := range details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// The journal is read by more people than write to it, and an entry is about
// what was done, not about the words somebody typed. The details of the entry
// are a closed set, and the name of the token is not in it: the token's own row
// holds the name, and the entry points at the row.
func TestCreatingATokenIsRecordedWithAClosedSetOfDetails(t *testing.T) {
	token, actor, now := journaled()

	entry := APITokenCreatedEntry(&actor, token, now)

	if entry.Action != ActionAPITokenCreated || entry.Outcome != Allowed || entry.TargetType != TargetAPIToken {
		t.Errorf("entry is %q / %q / %q", entry.Action, entry.Outcome, entry.TargetType)
	}
	if entry.OrgID != token.OrgID || entry.ProjectID == nil || *entry.ProjectID != token.ProjectID {
		t.Errorf("the entry belongs to org %s project %v, want %s %s", entry.OrgID, entry.ProjectID, token.OrgID, token.ProjectID)
	}
	if entry.TargetID == nil || *entry.TargetID != token.ID {
		t.Errorf("the entry points at %v, want the token %s", entry.TargetID, token.ID)
	}
	if entry.ActorID == nil || *entry.ActorID != actor || !entry.At.Equal(now) {
		t.Errorf("author %v at %v, want %s at %v", entry.ActorID, entry.At, actor, now)
	}

	if got, want := keysOf(entry.Details), []string{"expires_at", "prefix"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("details %v, want exactly %v", got, want)
	}
	if entry.Details["prefix"] != token.Prefix {
		t.Errorf("prefix %q, want %q", entry.Details["prefix"], token.Prefix)
	}
	if entry.Details["expires_at"] != "2027-02-18T12:00:00Z" {
		t.Errorf("expires_at %q, want the date in UTC", entry.Details["expires_at"])
	}
	for k, v := range entry.Details {
		if strings.Contains(v, token.Name) || strings.Contains(v, "payments") {
			t.Errorf("detail %q carries the name of the token: %q", k, v)
		}
	}
}

func TestRevokingATokenIsRecordedWithItsPrefixOnly(t *testing.T) {
	token, actor, now := journaled()

	entry := APITokenRevokedEntry(&actor, token, now)

	if entry.Action != ActionAPITokenRevoked || entry.Outcome != Allowed || entry.TargetType != TargetAPIToken {
		t.Errorf("entry is %q / %q / %q", entry.Action, entry.Outcome, entry.TargetType)
	}
	if entry.TargetID == nil || *entry.TargetID != token.ID || entry.ProjectID == nil || *entry.ProjectID != token.ProjectID {
		t.Errorf("the entry points at %v in %v, want %s in %s", entry.TargetID, entry.ProjectID, token.ID, token.ProjectID)
	}
	if got := keysOf(entry.Details); strings.Join(got, ",") != "prefix" {
		t.Errorf("details %v, want exactly [prefix]", got)
	}
	if strings.Contains(entry.Details["prefix"], "payments") {
		t.Error("the entry carries the name of the token")
	}
}

// An entry with no author is Ballast acting for nobody. A person revoking a
// token is somebody, and the builder takes the author as it is given.
func TestTheEntryKeepsItsAuthorEvenWhenThereIsNone(t *testing.T) {
	token, _, now := journaled()

	if entry := APITokenRevokedEntry(nil, token, now); entry.ActorID != nil {
		t.Errorf("an entry built with no author names %v", entry.ActorID)
	}
}
