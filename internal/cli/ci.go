package cli

import (
	"strings"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// DetectCI reads what a CI system says about the job it is running, from the
// variables it sets, and returns it as the run's CI metadata. It also returns the
// warnings to print.
//
// It knows GitHub Actions and GitLab CI, and nothing else: a CI it does not know
// has no variables it could read the same way, and guessing at them would be
// declaring things nobody said.
//
// What it returns is declared and never proved, and it is optional. Whatever is
// wrong with it, the run still starts: metadata that the server would refuse is not
// sent, and a warning says which field and what rule, and never what was in it —
// the value is somebody else's text and may be a secret.
//
// The check is the domain's own, ParseRunCI, so that this tool and the server cannot
// disagree about what is acceptable, and what is sent is what the server keeps as it
// is: tidy, with the commit in lower case.
//
// Only the variables named here are read. The environment of a pipeline is full of
// secrets, BALLAST_TOKEN among them, and a function that reads the environment is
// asked for a name at a time, from a list written out.
func DetectCI(env func(string) string) (*CIMetadata, []string) {
	gitHub := env("GITHUB_ACTIONS") == "true"
	gitLab := env("GITLAB_CI") == "true"

	var candidate CIMetadata
	switch {
	case gitHub && gitLab:
		// A machine that says it is two systems at once is not telling the truth
		// about one of them, and declaring the wrong one is worse than none.
		return nil, []string{"CI metadata was not sent: both GITHUB_ACTIONS and GITLAB_CI are set, and it is not known which system this is"}
	case gitHub:
		candidate = fromGitHubActions(env)
	case gitLab:
		candidate = fromGitLabCI(env)
	default:
		return nil, nil
	}

	checked, err := domain.ParseRunCI(domain.RunCI{
		Provider:   domain.CIProvider(candidate.Provider),
		Repository: candidate.Repository,
		Branch:     candidate.Branch,
		Commit:     candidate.Commit,
		JobURL:     candidate.JobURL,
	})
	if err != nil {
		// The error names the field and the rule. It never carries the value.
		return nil, []string{"CI metadata was not sent: " + err.Error()}
	}

	return &CIMetadata{
		Provider:   string(checked.Provider),
		Repository: checked.Repository,
		Branch:     checked.Branch,
		Commit:     checked.Commit,
		JobURL:     checked.JobURL,
	}, nil
}

func fromGitHubActions(env func(string) string) CIMetadata {
	metadata := CIMetadata{
		Provider:   string(domain.CIProviderGitHubActions),
		Repository: strings.TrimSpace(env("GITHUB_REPOSITORY")),
		Commit:     strings.TrimSpace(env("GITHUB_SHA")),
	}

	// A tag is not a branch, and is not declared as one. On a pull request the ref
	// name is "12/merge", which exists only on GitHub's side; the branch somebody
	// pushed is the head ref.
	if strings.TrimSpace(env("GITHUB_REF_TYPE")) != "tag" {
		metadata.Branch = firstOf(env("GITHUB_HEAD_REF"), env("GITHUB_REF_NAME"))
	}

	// A link to the job is built from its three parts, or not at all: a server and
	// no run is not a link to anything.
	server := strings.TrimRight(strings.TrimSpace(env("GITHUB_SERVER_URL")), "/")
	run := strings.TrimSpace(env("GITHUB_RUN_ID"))
	if server != "" && metadata.Repository != "" && run != "" {
		metadata.JobURL = server + "/" + metadata.Repository + "/actions/runs/" + run
	}
	return metadata
}

func fromGitLabCI(env func(string) string) CIMetadata {
	metadata := CIMetadata{
		Provider:   string(domain.CIProviderGitLab),
		Repository: strings.TrimSpace(env("CI_PROJECT_PATH")),
		Commit:     strings.TrimSpace(env("CI_COMMIT_SHA")),
		JobURL:     strings.TrimSpace(env("CI_JOB_URL")),
	}

	// A tag pipeline has no branch. A merge request pipeline has none of its own
	// either: its ref is one GitLab made, and the branch is the one it came from.
	if strings.TrimSpace(env("CI_COMMIT_TAG")) == "" {
		metadata.Branch = firstOf(
			env("CI_COMMIT_BRANCH"), env("CI_MERGE_REQUEST_SOURCE_BRANCH_NAME"), env("CI_COMMIT_REF_NAME"))
	}
	return metadata
}

// firstOf is the first of the values that says anything.
func firstOf(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
