package domain

import (
	"errors"
	"strings"
	"testing"
)

// What a pipeline says about itself when it launches a run: which CI, which
// repository, branch and commit, and the link to the job. It is declared by
// whoever calls, not proved to Ballast by anybody, so what these tests hold in
// place is what Ballast will and will not keep of it:
//
//   - a closed set of fields, each bounded;
//   - text that is shown as text and nothing else — no control character, no
//     invisible character that makes a branch read as something it is not;
//   - a link that is an https address with no credentials in it;
//   - an error that says which field is wrong and never repeats what was sent,
//     which may be a secret or somebody else's text.
//
// Written before the code, which is why they do not compile yet.

func githubRun() RunCI {
	return RunCI{
		Provider:   CIProviderGitHubActions,
		Repository: "myballast/ballast",
		Branch:     "feature/ci-metadata",
		Commit:     "3f9c2a1b7d4e5f60718293a4b5c6d7e8f9012345",
		JobURL:     "https://github.com/myballast/ballast/actions/runs/123456789",
	}
}

func TestAWellFormedRunIsKeptAsItWasSent(t *testing.T) {
	got, err := ParseRunCI(githubRun())
	if err != nil {
		t.Fatalf("ParseRunCI: %v", err)
	}
	if got != githubRun() {
		t.Errorf("got %+v, want it kept as sent", got)
	}
}

// A pipeline that cannot say everything says what it can: a detached checkout
// has no branch, a script has no job page. Only the provider is required, for
// without it there is nothing to say what the rest is about.
func TestOnlyTheProviderIsRequired(t *testing.T) {
	for _, provider := range []CIProvider{CIProviderGitHubActions, CIProviderGitLab, CIProviderOther} {
		got, err := ParseRunCI(RunCI{Provider: provider})
		if err != nil || got.Provider != provider {
			t.Errorf("%q alone: %+v, %v", provider, got, err)
		}
	}
}

// Spaces and line breaks around a value are dropped, for an environment variable
// of a CI often ends with one. A line break inside a value is another matter, and
// is refused below.
func TestWhatIsKeptIsTidied(t *testing.T) {
	got, err := ParseRunCI(RunCI{
		Provider:   "  gitlab_ci ",
		Repository: " group/sub-group/project\n",
		Branch:     "\tmain ",
		// A commit is written lower case by git, and a pipeline that upper-cases
		// it is still talking about the same one.
		Commit: " 3F9C2A1B ",
		// The query and the fragment may carry a token, and say nothing about
		// which job it is.
		JobURL: "https://gitlab.com/group/project/-/jobs/42?token=abc123#L10",
	})
	if err != nil {
		t.Fatalf("ParseRunCI: %v", err)
	}

	want := RunCI{
		Provider:   CIProviderGitLab,
		Repository: "group/sub-group/project",
		Branch:     "main",
		Commit:     "3f9c2a1b",
		JobURL:     "https://gitlab.com/group/project/-/jobs/42",
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTheSchemeAndHostOfTheLinkAreLowerCaseAndThePortIsKept(t *testing.T) {
	got, err := ParseRunCI(RunCI{Provider: CIProviderOther, JobURL: "HTTPS://CI.Example.test:8443/job/7"})
	if err != nil {
		t.Fatalf("ParseRunCI: %v", err)
	}
	if want := "https://ci.example.test:8443/job/7"; got.JobURL != want {
		t.Errorf("link %q, want %q", got.JobURL, want)
	}
}

func TestAnythingThatIsNotAProviderIsRefused(t *testing.T) {
	for _, provider := range []CIProvider{"", " ", "github", "GitHub", "GITHUB_ACTIONS", "jenkins", "github\n_actions", "other ; drop"} {
		if _, err := ParseRunCI(RunCI{Provider: provider}); !errors.Is(err, ErrInvalidRunCI) {
			t.Errorf("provider %q: error %v, want ErrInvalidRunCI", provider, err)
		}
	}
}

func TestARepositoryIsAPathOfPlainNames(t *testing.T) {
	for _, ok := range []string{"a", "owner/repo", "org/my.repo_name-2", "group/sub/sub2/project", strings.Repeat("a", 200)} {
		if _, err := ParseRunCI(RunCI{Provider: CIProviderOther, Repository: ok}); err != nil {
			t.Errorf("%.40q was refused: %v", ok, err)
		}
	}

	for _, bad := range []string{
		strings.Repeat("a", 201),
		"/owner/repo", "owner/repo/", "owner//repo", "../etc/passwd", "owner/..",
		"owner/repo name", "owner/re\npo", "owner/repo`id`", "owner/$HOME", "<script>/x", "own;er/repo",
		"https://github.com/owner/repo", "owner/rép",
	} {
		if _, err := ParseRunCI(RunCI{Provider: CIProviderOther, Repository: bad}); !errors.Is(err, ErrInvalidRunCI) {
			t.Errorf("repository %.40q: error %v, want ErrInvalidRunCI", bad, err)
		}
	}
}

func TestABranchIsTextThatReadsAsWhatItIs(t *testing.T) {
	for _, ok := range []string{"main", "feature/x", "refs/pull/12/merge", "release-1.2.3", "fix_ünïcode", strings.Repeat("b", 255)} {
		if _, err := ParseRunCI(RunCI{Provider: CIProviderOther, Branch: ok}); err != nil {
			t.Errorf("%.40q was refused: %v", ok, err)
		}
	}

	for name, bad := range map[string]string{
		"too long":           strings.Repeat("b", 256),
		"a line break":       "main\nBallast: passed",
		"a tab inside":       "ma\tin",
		"a space inside":     "my branch",
		"a NUL":              "ma\x00in",
		"an escape sequence": "main\x1b[31m",
		// The one that reads as something it is not: a right-to-left override makes
		// "release" appear as "esaeler" and whatever follows as the other way round.
		"a bidirectional override": "release\u202egnp.exe",
		"a zero-width space":       "ma\u200bin",
		"a byte order mark":        "\ufeffmain",
		"a line separator":         "ma\u2028in",
		"a paragraph separator":    "ma\u2029in",
		"invalid UTF-8":            "ma\xffin",
	} {
		if _, err := ParseRunCI(RunCI{Provider: CIProviderOther, Branch: bad}); !errors.Is(err, ErrInvalidRunCI) {
			t.Errorf("branch with %s: error %v, want ErrInvalidRunCI", name, err)
		}
	}
}

func TestACommitIsAHexadecimalHash(t *testing.T) {
	for _, ok := range []string{"abcdef1", "0123456789abcdef", strings.Repeat("a", 40), strings.Repeat("0", 64)} {
		if _, err := ParseRunCI(RunCI{Provider: CIProviderOther, Commit: ok}); err != nil {
			t.Errorf("%.20q was refused: %v", ok, err)
		}
	}

	for _, bad := range []string{
		"abcdef",                // six characters say too little
		strings.Repeat("a", 65), // and 65 are no hash
		"abcdefg",
		"main",
		"abc def1",
		"abc\ndef1",
		"abcdef1; rm -rf /",
		"ａｂｃｄｅｆ１", // full-width digits that look like hex
	} {
		if _, err := ParseRunCI(RunCI{Provider: CIProviderOther, Commit: bad}); !errors.Is(err, ErrInvalidRunCI) {
			t.Errorf("commit %.20q: error %v, want ErrInvalidRunCI", bad, err)
		}
	}
}

func TestALinkIsAnHTTPSAddressAndNothingElse(t *testing.T) {
	for _, bad := range []string{
		"http://ci.example.test/job/1",
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"file:///etc/passwd",
		"ftp://ci.example.test/job",
		"//ci.example.test/job",
		"ci.example.test/job",
		"/job/1",
		"https://",
		"https:///job/1",
		"https:// ci.example.test/job",
		"https://ci.example.test/job one",
		"https://ci.example.test/jo\nb",
		"https://ci.example.test/\x00",
		"https://ci.example.test:99999/job",
		"https://ci.example.test:port/job",
		"https://[::1]/job",
		// A host written in letters that only look like ASCII: the link would go
		// somewhere other than where it reads.
		"https://exаmple.com/job",
		"https://exämple.com/job",
		"HTTPS:\\\\ci.example.test\\job",
		"https://" + strings.Repeat("a", 500) + ".test/job",
	} {
		if _, err := ParseRunCI(RunCI{Provider: CIProviderOther, JobURL: bad}); !errors.Is(err, ErrInvalidRunCI) {
			t.Errorf("link %.60q: error %v, want ErrInvalidRunCI", bad, err)
		}
	}
}

// Credentials in a link are a secret that leaked into a pipeline's configuration.
// They are refused, and not quietly dropped: dropping them would hide it from the
// person who has to rotate it.
func TestACredentialInTheLinkIsRefusedNotDropped(t *testing.T) {
	for _, bad := range []string{
		"https://user:hunter2@ci.example.test/job/1",
		"https://hunter2@ci.example.test/job/1",
		"https://:hunter2@ci.example.test/job/1",
	} {
		_, err := ParseRunCI(RunCI{Provider: CIProviderOther, JobURL: bad})
		if !errors.Is(err, ErrInvalidRunCI) {
			t.Errorf("%q: error %v, want ErrInvalidRunCI", bad, err)
			continue
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("the error repeats the credential: %v", err)
		}
	}
}

// What was sent is somebody else's text, and may be a secret. The error says
// which field is wrong and what the rule is, and never the value.
func TestAnErrorNamesTheFieldAndNeverRepeatsWhatWasSent(t *testing.T) {
	const marker = "SENSITIVE-9f3a"

	cases := map[string]RunCI{
		"repository": {Provider: CIProviderOther, Repository: marker + " with spaces"},
		"branch":     {Provider: CIProviderOther, Branch: marker + "\nx"},
		"commit":     {Provider: CIProviderOther, Commit: marker},
		"job_url":    {Provider: CIProviderOther, JobURL: "http://" + marker + ".test/job"},
		"provider":   {Provider: CIProvider(marker)},
	}

	for field, in := range cases {
		_, err := ParseRunCI(in)
		if !errors.Is(err, ErrInvalidRunCI) {
			t.Fatalf("%s: error %v, want ErrInvalidRunCI", field, err)
		}
		if strings.Contains(err.Error(), marker) {
			t.Errorf("%s: the error repeats what was sent: %v", field, err)
		}
		if !strings.Contains(err.Error(), field) {
			t.Errorf("%s: the error does not say which field is wrong: %v", field, err)
		}
	}
}

// --- The run -------------------------------------------------------------------------

func TestARunLaunchedWithCIDataKeepsIt(t *testing.T) {
	ci := githubRun()

	run := aRun(nil).WithCI(ci)

	if run.CI == nil || *run.CI != ci {
		t.Errorf("the run holds %+v, want %+v", run.CI, ci)
	}
}

// The run keeps a copy. What the caller does with theirs afterwards does not
// rewrite what was measured.
func TestARunKeepsACopyOfWhatItWasGiven(t *testing.T) {
	ci := githubRun()
	run := aRun(nil).WithCI(ci)

	ci.Branch = "changed-after-the-fact"

	if run.CI.Branch != "feature/ci-metadata" {
		t.Errorf("the run follows a later change: %q", run.CI.Branch)
	}
}

func TestARunLaunchedWithoutCIDataHasNone(t *testing.T) {
	if run := aRun(nil); run.CI != nil {
		t.Errorf("a run built without CI data holds %+v", run.CI)
	}
}
