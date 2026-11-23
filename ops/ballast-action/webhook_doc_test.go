package ballastaction

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// The page "Start a run from a signed request" is what somebody follows to sign a request
// from a tool that is neither GitHub nor GitLab. What these tests hold in place:
//
//   - every figure on it is a constant of the code, so that a page and a limit cannot
//     drift apart;
//   - it is shown twice, by GitHub and by the site, and the two are the same bytes;
//   - the site's page is public, and reads the copy that is tested here;
//   - nothing on it is a value that looks like a secret, and every link goes somewhere.
//
// That the shell example runs, and that Ballast accepts what it sends, is held by a test
// that runs it against the real API (internal/e2e).

const (
	webhookPage = "../../docs/webhook.md"
	webhookCopy = "../../web/src/features/docs/content/webhook.md"
	webhookSite = "../../web/src/app/(docs)/docs/webhook/page.tsx"
)

func TestTheFiguresOfTheWebhookPageAreTheConstantsOfTheCode(t *testing.T) {
	page := flat(read(t, webhookPage))

	for _, want := range []string{
		fmt.Sprintf("within %d minutes of Ballast's clock", int(domain.WebhookTimestampWindow.Minutes())),
		fmt.Sprintf("remembered for %d minutes", int(domain.WebhookDeliveryRetention.Minutes())),
		fmt.Sprintf("%d KiB at most", domain.MaxWebhookBody>>10),
		fmt.Sprintf("| Runs per hour | %d per webhook, and %d for the organization |", domain.MaxRunsPerUserPerWindow, domain.MaxRunsPerOrgPerWindow),
		fmt.Sprintf("| Runs waiting | %d for the organization |", domain.MaxQueuedRunsPerOrg),
		"16 to 64 letters, digits",
		"`X-Ballast-Timestamp`", "`X-Ballast-Delivery`", "`X-Ballast-Signature`",
		domain.WebhookSignatureVersion + "=",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not say %q", want)
		}
	}
	if domain.RateWindow.Hours() != 1 {
		t.Error("the window of the rate limit changed: the page says an hour")
	}
}

func TestTheSiteShowsTheSameWebhookPageAsGitHub(t *testing.T) {
	if read(t, webhookPage) != read(t, webhookCopy) {
		t.Error("web/src/features/docs/content/webhook.md is not docs/webhook.md: cp docs/webhook.md web/src/features/docs/content/webhook.md")
	}
}

func TestTheWebhookRouteReadsTheCopyAndIsOpenToAnybody(t *testing.T) {
	if !strings.Contains(read(t, webhookSite), `"src/features/docs/content/webhook.md"`) {
		t.Error("the route does not read the copy of the page")
	}
	if !strings.Contains(read(t, proxyRule), `"/docs/webhook"`) {
		t.Error("the proxy does not let /docs/webhook through without a session")
	}
}

func TestNothingOnTheWebhookPageIsASecret(t *testing.T) {
	if regexp.MustCompile(`blw_[A-Za-z0-9_-]{8,}`).MatchString(read(t, webhookPage)) {
		t.Error("the page holds something that looks like a signing secret")
	}
}

func TestEveryLinkOfTheWebhookPageGoesSomewhere(t *testing.T) {
	page := read(t, webhookPage)

	for _, m := range regexp.MustCompile(`\]\(([^)]+)\)`).FindAllStringSubmatch(page, -1) {
		target := m[1]
		switch {
		case strings.HasPrefix(target, "https://"):
			if !strings.HasPrefix(target, "https://github.com/myballast/") {
				t.Errorf("a link leaves the project: %s", target)
			}
		case strings.HasPrefix(target, "#"):
			if !hasHeading(page, strings.TrimPrefix(target, "#")) {
				t.Errorf("the link %s points at a heading that is not there", target)
			}
		default:
			if _, err := os.Stat("../../docs/" + strings.SplitN(target, "#", 2)[0]); err != nil {
				t.Errorf("the link %s goes nowhere", target)
			}
		}
	}
}

func TestThePagesLeadToTheWebhookPage(t *testing.T) {
	for _, page := range []string{"../../docs/ci.md", "../../docs/cli.md"} {
		if !strings.Contains(read(t, page), "webhook.md") {
			t.Errorf("%s does not lead to the webhook page", page)
		}
	}
}
