package cli

import (
	"fmt"
	"regexp"
	"runtime"
)

// Version is the release this binary was built from, put in at link time:
//
//	go build -ldflags "-X github.com/youssefmahersi/ballast/internal/cli.Version=v1.2.3"
//
// A build with nothing injected says "dev", so that one nobody released can never
// be taken for one that was.
var Version = "dev"

// versionShape is what a version may look like. It is text from a build, and a build
// is not always right: a line break in it would be refused by the HTTP client and
// every request of the tool would fail with it, and a control character would end up
// in somebody's log. Anything that is not a version is a development build.
var versionShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// version is Version, or "dev" when Version is not a version.
func version() string {
	if versionShape.MatchString(Version) {
		return Version
	}
	return "dev"
}

// userAgent says what a request comes from, so that a server's log can tell a
// pipeline from a browser and one release from another. It says nothing else about
// the machine.
func userAgent() string {
	return "ballast-cli/" + version()
}

// versionLine is what "ballast version" prints: the release, and what it was built
// for and with.
func versionLine() string {
	return fmt.Sprintf("ballast %s (%s/%s, %s)", version(), runtime.GOOS, runtime.GOARCH, runtime.Version())
}
