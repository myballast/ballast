package ballastaction

import (
	"strings"
	"testing"
)

// The page "Run Ballast from CI" is shown twice: by GitHub, from docs/ci.md, and by the
// site, at /docs/ci, from a copy under web/ (the image of the front end is built from
// web/ alone, so it cannot read docs/). Two copies of a page drift, and the one that is
// wrong is the one somebody follows. What these tests hold in place:
//
//   - the copy is the page, byte for byte;
//   - the site's page is public, and the landing page leads to it from its menu and its
//     footer, so that someone who has not signed up can find out how to use Ballast;
//   - the route reads the copy that is tested here, and not another file.

const (
	siteCopy    = "../../web/src/features/docs/content/ci.md"
	sitePage    = "../../web/src/app/(docs)/docs/ci/page.tsx"
	proxyRule   = "../../web/src/proxy-rule.ts"
	landingPage = "../../web/public/landing.html"
)

func TestTheSiteShowsTheSamePageAsGitHub(t *testing.T) {
	if read(t, ciPage) != read(t, siteCopy) {
		t.Errorf("web/src/features/docs/content/ci.md is not docs/ci.md: copy the page again (cp docs/ci.md %s)", "web/src/features/docs/content/ci.md")
	}
}

func TestTheRouteReadsTheCopyAndIsOpenToAnybody(t *testing.T) {
	page := read(t, sitePage)
	if !strings.Contains(page, `"src/features/docs/content/ci.md"`) {
		t.Error("the route does not read the copy of the page")
	}

	rule := read(t, proxyRule)
	if !strings.Contains(rule, `"/docs/ci"`) {
		t.Error("the proxy does not let /docs/ci through without a session")
	}
}

func TestTheLandingPageLeadsToTheDocumentationFromItsMenuAndItsFooter(t *testing.T) {
	landing := read(t, landingPage)

	if got := strings.Count(landing, `href="/docs/ci"`); got != 2 {
		t.Errorf("the landing page links to /docs/ci %d times, want twice (menu and footer)", got)
	}
}

func TestAnExternalLinkOfThePageOpensWithoutGivingTheSiteAway(t *testing.T) {
	view := read(t, "../../web/src/features/docs/components/markdown-view.tsx")

	if !strings.Contains(view, `rel: "noopener noreferrer"`) || !strings.Contains(view, "safeHref(inline.href)") {
		t.Error("a link of a page is not drawn through safeHref with rel=noopener noreferrer")
	}
}
