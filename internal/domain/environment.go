package domain

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Environment is a target a project is allowed to send load to.
//
// Only the two ceilings are modelled here, because they are what a load profile
// is clamped against and nothing else in the domain needs the rest yet. The
// full entity — base URL, verification state, secrets, allowed hosts — belongs
// to the environment use cases and arrives with them.
type Environment struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	// BaseURL is what the scenario reads instead of hardcoding a host.
	BaseURL string
	// VerificationStatus says whether the organization proved it owns this
	// target and an administrator approved it. Anything but Verified means no
	// load may be sent here.
	VerificationStatus VerificationStatus
	// VerificationToken is the challenge this target has to serve to prove
	// somebody owns it.
	//
	// Unlike a secret, it is meant to be published: it ends up in a file on the
	// target's own web server. It is unguessable all the same, so that nobody
	// can serve a file in advance for an environment they do not own. Kept out
	// of the environment's own API response, and out of every log.
	VerificationToken string
	// MaxVUs is the most virtual users this environment tolerates.
	MaxVUs int
	// MaxDurationS is the longest a run may last here, in seconds.
	MaxDurationS int
	// Window is the hours this target tolerates being loaded. Nil means any
	// hour.
	Window *AllowedWindow
	// VerifiedAt and VerifiedBy are set together with the verified status, and
	// migration 001 refuses any other combination: an approval always says when
	// and by whom.
	VerifiedAt *time.Time
	VerifiedBy *uuid.UUID
	CreatedAt  time.Time
}

// AllowedWindow is the stretch of the day an environment accepts load.
//
// Held as minutes since midnight and a zone name rather than as two instants,
// because the rule is about wall-clock time: "not during office hours" means the
// office hours of the people who would have to answer for the target. A window
// whose start is after its end crosses midnight, which is the one most people
// actually want.
type AllowedWindow struct {
	StartMinute int
	EndMinute   int
	// Zone is an IANA name, such as Europe/Paris.
	Zone string
}

const minutesInADay = 24 * 60

// Validate refuses a window Ballast will not store.
func (w AllowedWindow) Validate() error {
	switch {
	case w.StartMinute < 0 || w.StartMinute >= minutesInADay:
		return fmt.Errorf("%w: the window starts at minute %d of the day", ErrInvalidEnvironment, w.StartMinute)
	case w.EndMinute < 0 || w.EndMinute >= minutesInADay:
		return fmt.Errorf("%w: the window ends at minute %d of the day", ErrInvalidEnvironment, w.EndMinute)
	case w.StartMinute == w.EndMinute:
		// It would refuse every run. Nobody means that; the way to allow every
		// hour is to declare no window at all.
		return fmt.Errorf("%w: the window is empty", ErrInvalidEnvironment)
	case strings.TrimSpace(w.Zone) == "":
		return fmt.Errorf("%w: the window names no timezone", ErrInvalidEnvironment)
	}

	if _, err := time.LoadLocation(w.Zone); err != nil {
		return fmt.Errorf("%w: %q is not a timezone this machine knows", ErrInvalidEnvironment, w.Zone)
	}

	return nil
}

// Contains reports whether t falls inside the window, read in the window's own
// zone.
//
// The start is inclusive and the end is not, so two windows that meet do not
// overlap on the minute they share.
//
// A zone the machine cannot resolve means the rule cannot be evaluated, and the
// answer to that is no rather than yes: a window exists to keep load out of
// certain hours, so failing open would silently remove it. Validation refuses
// such a zone on the way in, which leaves a database restored onto a machine
// with no zone database — where no is still the right answer.
func (w AllowedWindow) Contains(t time.Time) bool {
	zone, err := time.LoadLocation(w.Zone)
	if err != nil {
		return false
	}

	local := t.In(zone)
	minute := local.Hour()*60 + local.Minute()

	if w.StartMinute < w.EndMinute {
		return minute >= w.StartMinute && minute < w.EndMinute
	}
	// The window crosses midnight: it is everything from the start to the end
	// of the day, plus everything from the start of the day to the end.
	return minute >= w.StartMinute || minute < w.EndMinute
}

// RemainingAt is how much of the window is left at t, and zero when t falls
// outside it.
//
// Computed from the end instant in the window's own zone rather than by
// subtracting minutes of the clock, so a window that spans a daylight-saving
// change reports the hours that will actually pass. On the night the clocks go
// forward, a window open until four has one hour less in it than the clock says.
func (w AllowedWindow) RemainingAt(t time.Time) time.Duration {
	zone, err := time.LoadLocation(w.Zone)
	if err != nil {
		return 0
	}

	local := t.In(zone)
	if !w.Contains(local) {
		return 0
	}

	end := time.Date(local.Year(), local.Month(), local.Day(),
		w.EndMinute/60, w.EndMinute%60, 0, 0, zone)
	// Already past today's closing time, so the window is one that crosses
	// midnight and closes tomorrow.
	if !end.After(local) {
		end = end.AddDate(0, 0, 1)
	}

	return end.Sub(local)
}

// RequireWindowAllows refuses a run of runFor that may not start at t.
//
// There is deliberately no way to ask only about the starting instant. A window
// that let a run begin inside it and finish hours outside it would be a rule
// about one minute of the day rather than about the hours a target tolerates
// load — and that was exactly the hole here: a two hour run accepted at five to
// six against a window closing at six.
//
// The answer is a refusal rather than a shortened run. Load parameters are
// clamped to the environment's ceilings, but those are about how hard; cutting a
// two hour soak down to eight minutes because of the hour would hand somebody a
// measurement of nothing under the name they asked for. Clamping here would also
// mean rewriting what is frozen on the run, which is the one thing a run's record
// must never do.
//
// An environment that declared no window takes load at any hour, for any length.
func (e Environment) RequireWindowAllows(t time.Time, runFor time.Duration) error {
	if e.Window == nil {
		return nil
	}

	if !e.Window.Contains(t) {
		return fmt.Errorf("%w: it is open from %s to %s in %s",
			ErrOutsideAllowedWindow,
			clock(e.Window.StartMinute), clock(e.Window.EndMinute), e.Window.Zone)
	}

	// A run of no length is not a run; validation refuses one long before here,
	// and the containment above is the whole question for it.
	if runFor <= 0 {
		return nil
	}

	if left := e.Window.RemainingAt(t); runFor > left {
		return fmt.Errorf("%w: it closes in %s and the run would last %s",
			ErrOutsideAllowedWindow, left.Round(time.Second), runFor.Round(time.Second))
	}

	return nil
}

// clock renders minutes since midnight as HH:MM, for a message somebody reads.
func clock(minute int) string {
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}

const (
	maxEnvironmentNameLength = 120
	// The ceilings an environment may impose are themselves bounded: a target
	// cannot authorise more load than Ballast is willing to generate at all.
	maxConfigurableVUs       = 100_000
	maxConfigurableDurationS = 86_400
)

// ValidateEnvironment refuses an environment Ballast will not store. The window
// validates itself, and the verification of the target is a separate matter
// entirely.
func ValidateEnvironment(name, baseURL string, maxVUs, maxDurationS int) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("%w: the name is empty", ErrInvalidEnvironment)
	case len(name) > maxEnvironmentNameLength:
		return fmt.Errorf("%w: the name is longer than %d characters",
			ErrInvalidEnvironment, maxEnvironmentNameLength)
	case maxVUs <= 0:
		return fmt.Errorf("%w: max_vus must be at least 1", ErrInvalidEnvironment)
	case maxVUs > maxConfigurableVUs:
		return fmt.Errorf("%w: max_vus is above %d", ErrInvalidEnvironment, maxConfigurableVUs)
	case maxDurationS <= 0:
		return fmt.Errorf("%w: max_duration_s must be at least 1", ErrInvalidEnvironment)
	case maxDurationS > maxConfigurableDurationS:
		return fmt.Errorf("%w: max_duration_s is above %d", ErrInvalidEnvironment, maxConfigurableDurationS)
	}

	return validateBaseURL(baseURL)
}

// validateBaseURL refuses a base URL that is not a plain http address of a host.
//
// This is about what can be stored, not about who owns the host: proving that is
// the verification's job, and it needs something unambiguous to work from. A
// query string or a fragment would be silently lost the moment a scenario joins
// a path onto this, and credentials in the URL would end up in whatever logs the
// target keeps.
func validateBaseURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%w: the base URL is empty", ErrInvalidEnvironment)
	}
	if strings.ContainsAny(raw, " \t\r\n\x00") {
		return fmt.Errorf("%w: the base URL carries a space or a control character", ErrInvalidEnvironment)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: the base URL cannot be read", ErrInvalidEnvironment)
	}

	switch {
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return fmt.Errorf("%w: the base URL must be http or https", ErrInvalidEnvironment)
	case parsed.Host == "":
		return fmt.Errorf("%w: the base URL names no host", ErrInvalidEnvironment)
	case parsed.User != nil:
		return fmt.Errorf("%w: the base URL carries credentials; set them as secrets instead", ErrInvalidEnvironment)
	case parsed.RawQuery != "":
		return fmt.Errorf("%w: the base URL carries a query string", ErrInvalidEnvironment)
	case parsed.Fragment != "":
		return fmt.Errorf("%w: the base URL carries a fragment", ErrInvalidEnvironment)
	}

	return nil
}

// VerificationStatus tracks a target through proof and approval. The two are
// deliberately separate steps: automatic approval falls the day someone finds a
// way to serve a file on a domain that is not theirs.
type VerificationStatus string

// The values stored in environment.verification_status and constrained by
// migration 001.
const (
	VerificationPending  VerificationStatus = "pending"
	VerificationProofOK  VerificationStatus = "proof_ok"
	VerificationVerified VerificationStatus = "verified"
	VerificationFailed   VerificationStatus = "verification_failed"
)

// IsVerified reports whether load may be sent to this target at all.
func (e Environment) IsVerified() bool {
	return e.VerificationStatus == VerificationVerified
}

// HostPort is the target as the egress proxy's allowlist holds it: the host and
// the port traffic would actually go to.
//
// The port is part of what was approved. A tunnel to another port of the same
// machine is not the target an administrator looked at, so the default for the
// scheme is written out rather than left implicit.
func (e Environment) HostPort() (string, error) {
	parsed, err := url.Parse(e.BaseURL)
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("%w: the base URL names no host", ErrInvalidEnvironment)
	}

	port := parsed.Port()
	if port == "" {
		port = "80"
		if strings.EqualFold(parsed.Scheme, "https") {
			port = "443"
		}
	}

	return net.JoinHostPort(strings.ToLower(parsed.Hostname()), port), nil
}
