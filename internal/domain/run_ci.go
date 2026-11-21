package domain

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// What a pipeline says about itself when it launches a run: which CI, which
// repository, branch and commit, and the link to the job. It is declared by
// whoever calls and proved to Ballast by nobody, so what is kept of it is a closed
// set of fields, each bounded, shown as text and nothing else.

const (
	// MaxCIRepositoryLen is the longest repository path kept, in characters.
	MaxCIRepositoryLen = 200
	// MaxCIBranchLen is the longest branch name kept, in characters.
	MaxCIBranchLen = 255
	// MaxCIJobURLLen is the longest job link kept, in bytes.
	MaxCIJobURLLen = 500

	// MinCICommitLen is the shortest a commit hash may be: seven characters is
	// what a person abbreviates one to.
	MinCICommitLen = 7
	// MaxCICommitLen is the longest: 64 characters is a SHA-256.
	MaxCICommitLen = 64
)

// ErrInvalidRunCI reports CI data Ballast will not keep. It names the field and
// the rule, and never the value: what was sent is somebody else's text, and may be
// a credential. A mistake in the request, not a refusal.
var ErrInvalidRunCI = errors.New("invalid CI data")

// CIProvider is which CI launched the run. A closed set, so that two runs from the
// same provider are written the same way and can be grouped; anything else says
// "other".
type CIProvider string

const (
	// CIProviderGitHubActions is a run launched by a GitHub Actions workflow.
	CIProviderGitHubActions CIProvider = "github_actions"
	// CIProviderGitLab is a run launched by a GitLab CI pipeline.
	CIProviderGitLab CIProvider = "gitlab_ci"
	// CIProviderOther is any other CI, or a script.
	CIProviderOther CIProvider = "other"
)

// RunCI is the context a pipeline launched a run in. Only the provider is
// required: a detached checkout has no branch, and a script has no job page.
type RunCI struct {
	Provider   CIProvider
	Repository string
	Branch     string
	Commit     string
	JobURL     string
}

var repositoryPath = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)

var hostName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)

// maxHostLen is the longest a host name may be.
const maxHostLen = 253

// ParseRunCI checks what a pipeline sent and returns it in the form it is kept: tidy,
// the commit in lower case, and the link without its query or its fragment.
//
// Nothing here is ever written into a script or handed to a worker: it is shown, as
// text, on the run's page.
func ParseRunCI(in RunCI) (RunCI, error) {
	out := RunCI{
		Provider:   CIProvider(strings.TrimSpace(string(in.Provider))),
		Repository: strings.TrimSpace(in.Repository),
		Branch:     strings.TrimSpace(in.Branch),
		Commit:     strings.ToLower(strings.TrimSpace(in.Commit)),
		JobURL:     strings.TrimSpace(in.JobURL),
	}

	switch out.Provider {
	case CIProviderGitHubActions, CIProviderGitLab, CIProviderOther:
	default:
		return RunCI{}, ciError("provider", "must be github_actions, gitlab_ci or other")
	}

	if err := checkRepository(out.Repository); err != nil {
		return RunCI{}, err
	}
	if err := checkBranch(out.Branch); err != nil {
		return RunCI{}, err
	}
	if err := checkCommit(out.Commit); err != nil {
		return RunCI{}, err
	}

	jobURL, err := normalizeJobURL(out.JobURL)
	if err != nil {
		return RunCI{}, err
	}
	out.JobURL = jobURL

	return out, nil
}

func ciError(field, rule string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidRunCI, field, rule)
}

func checkRepository(repository string) error {
	if repository == "" {
		return nil
	}

	const rule = "must be a path of plain names, like owner/name"
	if len(repository) > MaxCIRepositoryLen || !repositoryPath.MatchString(repository) {
		return ciError("repository", rule)
	}
	// A segment made only of dots is a way up, and not a name.
	for _, segment := range strings.Split(repository, "/") {
		if strings.Trim(segment, ".") == "" {
			return ciError("repository", rule)
		}
	}
	return nil
}

// checkBranch refuses what would make a branch read as something it is not: a
// control character, any kind of space, and the invisible characters that move or
// hide text — a right-to-left override makes "release" read backwards and whatever
// follows read the other way round.
func checkBranch(branch string) error {
	if branch == "" {
		return nil
	}

	const rule = "must be at most 255 characters, with no spaces, control or invisible characters"
	if !utf8.ValidString(branch) || utf8.RuneCountInString(branch) > MaxCIBranchLen {
		return ciError("branch", rule)
	}
	for _, r := range branch {
		if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return ciError("branch", rule)
		}
	}
	return nil
}

func checkCommit(commit string) error {
	if commit == "" {
		return nil
	}

	const rule = "must be a hexadecimal hash of 7 to 64 characters"
	if len(commit) < MinCICommitLen || len(commit) > MaxCICommitLen {
		return ciError("commit", rule)
	}
	for _, r := range commit {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ciError("commit", rule)
		}
	}
	return nil
}

// normalizeJobURL keeps a link that is an https address with nothing in it that is
// not an address.
//
// Credentials are refused and not quietly dropped: they are a secret that leaked
// into a pipeline's configuration, and dropping them would hide it from the person
// who has to rotate it. The query and the fragment are dropped: they may carry a
// token, and say nothing about which job it is.
func normalizeJobURL(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	const rule = "must be an https address of a job"
	if len(raw) > MaxCIJobURLLen {
		return "", ciError("job_url", rule)
	}
	// Plain ASCII, with no space or control character and no backslash. A link
	// that needs anything else is percent-encoded, and a host written in letters
	// that only look like ASCII would send the reader somewhere other than where
	// the link reads.
	for _, r := range raw {
		if r <= ' ' || r >= 0x7f || r == '\\' {
			return "", ciError("job_url", rule)
		}
	}

	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.Scheme != "https" || u.Host == "" {
		return "", ciError("job_url", rule)
	}
	if u.User != nil {
		return "", ciError("job_url", "must not contain credentials")
	}

	host := strings.ToLower(u.Hostname())
	if len(host) > maxHostLen || !hostName.MatchString(host) {
		return "", ciError("job_url", rule)
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", ciError("job_url", rule)
		}
		host += ":" + port
	}

	u.Host = host
	u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = "", false, "", ""

	kept := u.String()
	if len(kept) > MaxCIJobURLLen {
		return "", ciError("job_url", rule)
	}
	return kept, nil
}
