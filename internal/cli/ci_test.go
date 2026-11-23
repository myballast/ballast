package cli

import (
	"sort"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// A run launched from a pipeline can say which pipeline it is: the provider, the
// repository, the branch, the commit and the job. The tool reads it from the
// variables the CI system sets, so nobody has to type it.
//
// What these tests hold in place:
//
//   - it is declared and never proved, and it is optional: whatever is wrong with it
//     never stops a run from starting, and never fails a pipeline;
//   - what is sent has been through the same check the server applies, so the server
//     never has to refuse it, and the check is the domain's and not a copy;
//   - nothing that is not CI metadata is read from the environment — above all the
//     token — and nothing that is wrong is printed back: a warning names the field and
//     the rule, never the value;
//   - what is not known is left out, rather than guessed: a tag is not a branch.
//
// Written before the code, which is why they do not compile yet.

func envOf(pairs ...string) func(string) string {
	values := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		values[pairs[i]] = pairs[i+1]
	}
	return func(name string) string { return values[name] }
}

const aCommit = "3f9c2a1b7d4e5f60718293a4b5c6d7e8f9012345"

func gitHubPush() []string {
	return []string{
		"GITHUB_ACTIONS", "true",
		"GITHUB_REPOSITORY", "myballast/ballast",
		"GITHUB_SHA", aCommit,
		"GITHUB_REF_NAME", "main", "GITHUB_REF_TYPE", "branch",
		"GITHUB_SERVER_URL", "https://github.com",
		"GITHUB_RUN_ID", "9876543210",
	}
}

func with(base []string, pairs ...string) []string {
	return append(append([]string{}, base...), pairs...)
}

// --- GitHub Actions --------------------------------------------------------------------------------------

func TestAGitHubActionsPushIsReadAsItIs(t *testing.T) {
	got, warnings := DetectCI(envOf(gitHubPush()...))

	want := CIMetadata{
		Provider: "github_actions", Repository: "myballast/ballast", Branch: "main",
		Commit: aCommit, JobURL: "https://github.com/myballast/ballast/actions/runs/9876543210",
	}
	if got == nil || *got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings %v", warnings)
	}
}

// On a pull request the ref name is "12/merge", a name that exists only on
// GitHub's side. The branch somebody pushed is the head ref.
func TestAGitHubPullRequestNamesTheBranchThatWasPushedAndNotTheMergeRef(t *testing.T) {
	got, _ := DetectCI(envOf(with(gitHubPush(),
		"GITHUB_EVENT_NAME", "pull_request", "GITHUB_HEAD_REF", "feature/ci-metadata", "GITHUB_REF_NAME", "12/merge")...))

	if got == nil || got.Branch != "feature/ci-metadata" {
		t.Errorf("got %+v, want the head ref", got)
	}
}

// A tag is not a branch. Declaring it as one would put a name on the run's page
// that somebody reads as a branch; the field is left empty.
func TestATagIsNotDeclaredAsABranch(t *testing.T) {
	got, warnings := DetectCI(envOf(with(gitHubPush(), "GITHUB_REF_TYPE", "tag", "GITHUB_REF_NAME", "v1.2.0")...))

	if got == nil || got.Branch != "" {
		t.Errorf("got %+v, want no branch", got)
	}
	if got != nil && got.Commit != aCommit {
		t.Errorf("the rest was lost with it: %+v", got)
	}
	if len(warnings) != 0 {
		t.Errorf("a tag is not a mistake: %v", warnings)
	}
}

func TestAGitHubEnterpriseServerBuildsItsOwnLink(t *testing.T) {
	got, _ := DetectCI(envOf(with(gitHubPush(), "GITHUB_SERVER_URL", "https://ghe.example.com")...))

	if got == nil || got.JobURL != "https://ghe.example.com/myballast/ballast/actions/runs/9876543210" {
		t.Errorf("got %+v", got)
	}
}

// What the environment does not give is left out and not made up.
func TestWhatTheEnvironmentDoesNotGiveIsLeftOut(t *testing.T) {
	got, warnings := DetectCI(envOf(
		"GITHUB_ACTIONS", "true", "GITHUB_REPOSITORY", "myballast/ballast", "GITHUB_SHA", aCommit))

	want := CIMetadata{Provider: "github_actions", Repository: "myballast/ballast", Commit: aCommit}
	if got == nil || *got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings %v", warnings)
	}
}

// A link needs all of its parts. A server address and no run is not a link to
// anything.
func TestALinkIsOnlyBuiltFromAllOfItsParts(t *testing.T) {
	for _, missing := range []string{"GITHUB_SERVER_URL", "GITHUB_REPOSITORY", "GITHUB_RUN_ID"} {
		pairs := gitHubPush()
		for i := 0; i < len(pairs); i += 2 {
			if pairs[i] == missing {
				pairs[i+1] = ""
			}
		}

		got, _ := DetectCI(envOf(pairs...))

		if got != nil && got.JobURL != "" {
			t.Errorf("without %s, a link was made up: %q", missing, got.JobURL)
		}
	}
}

// --- GitLab CI ------------------------------------------------------------------------------------------------

func gitLabBranch() []string {
	return []string{
		"GITLAB_CI", "true",
		"CI_PROJECT_PATH", "myballast/ballast",
		"CI_COMMIT_SHA", aCommit,
		"CI_COMMIT_BRANCH", "main", "CI_COMMIT_REF_NAME", "main",
		"CI_JOB_URL", "https://gitlab.com/myballast/ballast/-/jobs/1234567",
	}
}

func TestAGitLabBranchPipelineIsReadAsItIs(t *testing.T) {
	got, warnings := DetectCI(envOf(gitLabBranch()...))

	want := CIMetadata{
		Provider: "gitlab_ci", Repository: "myballast/ballast", Branch: "main",
		Commit: aCommit, JobURL: "https://gitlab.com/myballast/ballast/-/jobs/1234567",
	}
	if got == nil || *got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings %v", warnings)
	}
}

// A merge request pipeline has no branch of its own: its ref is one GitLab made.
func TestAGitLabMergeRequestNamesTheSourceBranch(t *testing.T) {
	got, _ := DetectCI(envOf(
		"GITLAB_CI", "true", "CI_PROJECT_PATH", "myballast/ballast", "CI_COMMIT_SHA", aCommit,
		"CI_COMMIT_REF_NAME", "refs/merge-requests/12/head",
		"CI_MERGE_REQUEST_SOURCE_BRANCH_NAME", "feature/ci-metadata"))

	if got == nil || got.Branch != "feature/ci-metadata" {
		t.Errorf("got %+v", got)
	}
}

func TestAGitLabTagPipelineHasNoBranch(t *testing.T) {
	got, _ := DetectCI(envOf(
		"GITLAB_CI", "true", "CI_PROJECT_PATH", "myballast/ballast", "CI_COMMIT_SHA", aCommit,
		"CI_COMMIT_REF_NAME", "v1.2.0", "CI_COMMIT_TAG", "v1.2.0"))

	if got == nil || got.Branch != "" {
		t.Errorf("got %+v, want no branch", got)
	}
}

// --- When it is wrong ---------------------------------------------------------------------------------------------

// A self-hosted GitLab on plain http, a branch with something hidden in its name, a
// link with a password in it: each is refused by the check the server applies, and
// is refused here first, so that the server never has to. The whole object goes —
// declared metadata with a field that cannot be trusted is not worth sending — and a
// warning says which field and why, without repeating what was in it.
func TestMetadataThatTheServerWouldRefuseIsNotSentAndTheWarningNamesTheFieldNotTheValue(t *testing.T) {
	cases := []struct {
		name     string
		env      []string
		field    string
		neverSay string
	}{
		{"a self-hosted GitLab on http", with(gitLabBranch(), "CI_JOB_URL", "http://gitlab.internal.example/g/p/-/jobs/1"),
			"job_url", "gitlab.internal.example"},
		{"a link with credentials", with(gitLabBranch(), "CI_JOB_URL", "https://deploy:s3cr3t-pass@gitlab.com/g/p/-/jobs/1"),
			"job_url", "s3cr3t-pass"},
		{"a branch with a direction mark in it", with(gitHubPush(), "GITHUB_REF_NAME", "release\u202etxt.exe"),
			"branch", "txt.exe"},
		{"a branch with a space in it", with(gitHubPush(), "GITHUB_REF_NAME", "my secret branch"),
			"branch", "my secret branch"},
		{"a commit that is not one", with(gitHubPush(), "GITHUB_SHA", "HEAD-of-the-hidden-branch"),
			"commit", "hidden-branch"},
		{"a repository that is not a path", with(gitHubPush(), "GITHUB_REPOSITORY", "my ballast/<script>"),
			"repository", "<script>"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, warnings := DetectCI(envOf(c.env...))

			if got != nil {
				t.Errorf("metadata the server would refuse was kept: %+v", got)
			}
			if len(warnings) != 1 {
				t.Fatalf("%d warning(s): %v", len(warnings), warnings)
			}
			if !strings.Contains(warnings[0], c.field) {
				t.Errorf("the warning does not name %s: %q", c.field, warnings[0])
			}
			if strings.Contains(warnings[0], c.neverSay) {
				t.Errorf("the warning repeats what was in the variable: %q", warnings[0])
			}
		})
	}
}

// The check is the domain's, so that the tool and the server cannot disagree about
// what is acceptable. A value this tool sends must be one the server keeps as it
// is.
func TestWhatIsSentIsWhatTheDomainKeeps(t *testing.T) {
	got, _ := DetectCI(envOf(with(gitHubPush(),
		"GITHUB_SHA", "  "+strings.ToUpper(aCommit)+"  ",
		"GITHUB_REF_NAME", "  main  ")...))

	if got == nil {
		t.Fatal("nothing was sent")
	}
	kept, err := domain.ParseRunCI(domain.RunCI{
		Provider: domain.CIProvider(got.Provider), Repository: got.Repository,
		Branch: got.Branch, Commit: got.Commit, JobURL: got.JobURL,
	})
	if err != nil {
		t.Fatalf("the server would refuse what was sent: %v", err)
	}
	if kept.Commit != got.Commit || kept.Branch != got.Branch || kept.JobURL != got.JobURL {
		t.Errorf("the server would keep something other than what was sent: sent %+v, kept %+v", got, kept)
	}
	if got.Commit != aCommit || got.Branch != "main" {
		t.Errorf("the values were not tidied: %+v", got)
	}
}

// --- What is not CI ------------------------------------------------------------------------------------------------

func TestWithoutAKnownCISystemNothingIsSent(t *testing.T) {
	for name, env := range map[string]func(string) string{
		"no variables at all":               envOf(),
		"a CI the tool does not know":       envOf("CI", "true", "JENKINS_URL", "https://jenkins.example.test", "BUILD_NUMBER", "17"),
		"GitHub Actions switched off":       envOf(append(gitHubPush(), "GITHUB_ACTIONS", "false")...),
		"something that only looks like it": envOf("GITHUB_ACTIONS", "yes", "GITHUB_REPOSITORY", "a/b"),
	} {
		t.Run(name, func(t *testing.T) {
			got, warnings := DetectCI(env)

			if got != nil || len(warnings) != 0 {
				t.Errorf("got %+v, warnings %v", got, warnings)
			}
		})
	}
}

// Two systems at once is a machine that is not telling the truth about one of them,
// and declaring the wrong one is worse than declaring none.
func TestTwoCISystemsAtOnceSendNothingAndSayWhy(t *testing.T) {
	got, warnings := DetectCI(envOf(append(gitHubPush(), "GITLAB_CI", "true")...))

	if got != nil {
		t.Errorf("got %+v", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "GITHUB_ACTIONS") || !strings.Contains(warnings[0], "GITLAB_CI") {
		t.Errorf("warnings %v", warnings)
	}
}

// --- What is read ------------------------------------------------------------------------------------------------------

// The tool runs in an environment full of secrets, and BALLAST_TOKEN is in it.
// What it reads is a short list, written out, and a variable outside the list is
// never asked for.
func TestOnlyTheVariablesItNeedsAreRead(t *testing.T) {
	allowed := map[string]bool{}
	for _, name := range []string{
		"GITHUB_ACTIONS", "GITHUB_REPOSITORY", "GITHUB_SHA", "GITHUB_REF_NAME", "GITHUB_REF_TYPE",
		"GITHUB_HEAD_REF", "GITHUB_SERVER_URL", "GITHUB_RUN_ID", "GITHUB_EVENT_NAME",
		"GITLAB_CI", "CI_PROJECT_PATH", "CI_COMMIT_SHA", "CI_COMMIT_BRANCH", "CI_COMMIT_REF_NAME",
		"CI_COMMIT_TAG", "CI_MERGE_REQUEST_SOURCE_BRANCH_NAME", "CI_JOB_URL",
	} {
		allowed[name] = true
	}

	for name, env := range map[string][]string{"GitHub": gitHubPush(), "GitLab": gitLabBranch(), "nothing": nil} {
		asked := map[string]bool{}
		values := envOf(append(env, "BALLAST_TOKEN", aToken, "AWS_SECRET_ACCESS_KEY", "x")...)

		DetectCI(func(variable string) string {
			asked[variable] = true
			return values(variable)
		})

		var names []string
		for variable := range asked {
			if !allowed[variable] {
				names = append(names, variable)
			}
		}
		sort.Strings(names)
		if len(names) > 0 {
			t.Errorf("%s: variables that are not CI metadata were read: %v", name, names)
		}
	}
}

// --- In the run -----------------------------------------------------------------------------------------------------------

func TestTheRunIsLaunchedWithWhatThePipelineSaid(t *testing.T) {
	b := &ballast{}

	got := wholeRun(t, b, func(_ *RunOptions, r *Runner) { r.Env = envOf(gitHubPush()...) })

	if got.code != ExitMet {
		t.Fatalf("exit %d\nstderr: %s", got.code, got.err)
	}
	ci, _ := b.launchBody()["ci"].(map[string]any)
	if ci["provider"] != "github_actions" || ci["repository"] != "myballast/ballast" || ci["commit"] != aCommit ||
		ci["branch"] != "main" || ci["job_url"] != "https://github.com/myballast/ballast/actions/runs/9876543210" {
		t.Errorf("ci %v", ci)
	}
}

func TestOutsideACISystemNoCIObjectIsSent(t *testing.T) {
	b := &ballast{}

	wholeRun(t, b, func(_ *RunOptions, r *Runner) { r.Env = envOf() })

	if _, present := b.launchBody()["ci"]; present {
		t.Errorf("a ci object was sent from nowhere: %v", b.launchBody())
	}
}

// Metadata that is wrong never stops a run, and never changes the code a pipeline
// reads: it is the one part of a launch that is optional.
func TestMetadataThatIsWrongNeverStopsTheRunAndOnlyWarns(t *testing.T) {
	b := &ballast{}

	got := wholeRun(t, b, func(_ *RunOptions, r *Runner) {
		r.Env = envOf(with(gitHubPush(), "GITHUB_REF_NAME", "release\u202etxt.exe")...)
	})

	if got.code != ExitMet {
		t.Errorf("exit %d, want 0: declared metadata decided the result of a run", got.code)
	}
	if b.posts.Load() != 1 {
		t.Errorf("the run was launched %d times, want once", b.posts.Load())
	}
	if _, present := b.launchBody()["ci"]; present {
		t.Errorf("metadata that was refused was sent: %v", b.launchBody())
	}
	if !strings.Contains(got.err, "branch") || !strings.Contains(got.err, "not sent") {
		t.Errorf("nothing says the metadata was dropped:\n%s", got.err)
	}
	if strings.Contains(got.all(), "txt.exe") || strings.Contains(got.all(), "\u202e") {
		t.Errorf("what was wrong was printed back:\n%s", got.all())
	}
}

// What was set by hand is the caller's, and detection does not override it.
func TestMetadataGivenByHandIsNotReplacedByWhatIsDetected(t *testing.T) {
	b := &ballast{}

	wholeRun(t, b, func(o *RunOptions, r *Runner) {
		o.CI = &CIMetadata{Provider: "other", Repository: "by/hand"}
		r.Env = envOf(gitHubPush()...)
	})

	ci, _ := b.launchBody()["ci"].(map[string]any)
	if ci["provider"] != "other" || ci["repository"] != "by/hand" {
		t.Errorf("ci %v", ci)
	}
}

// What the warning says goes where the tool's own words go, and carries the
// marker that says so.
func TestTheWarningIsTheToolsOwnWordsOnStderr(t *testing.T) {
	got := wholeRun(t, &ballast{}, func(_ *RunOptions, r *Runner) {
		r.Env = envOf(with(gitLabBranch(), "CI_JOB_URL", "http://gitlab.internal.example/g/p/-/jobs/1")...)
	})

	if !strings.Contains(got.err, "ballast: warning:") && !strings.Contains(got.err, "ballast: ") {
		t.Errorf("stderr:\n%s", got.err)
	}
	if strings.Contains(got.out, "not sent") {
		t.Errorf("the tool's words are on stdout:\n%s", got.out)
	}
}
