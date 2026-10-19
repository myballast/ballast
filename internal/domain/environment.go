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
	// VerifiedByName is the approver as a screen should say them. Filled when an
	// environment is read on its own, and empty on a list.
	VerifiedByName string
	// VerificationCheckedAt is when somebody last asked Ballast to fetch the
	// challenge, whatever came of it. Nil until the first attempt.
	VerificationCheckedAt *time.Time
	CreatedAt             time.Time
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
	// Days narrows the window to some days of the week, read in Zone. The zero
	// mask is every day, which is what every window written before days existed
	// means.
	//
	// A window that crosses midnight belongs to the day it starts: "mon–fri
	// 22:00–04:00" is five nights, and the one that starts on Friday ends on
	// Saturday morning.
	Days DayMask
}

const minutesInADay = 24 * 60

// DayMask is a set of days of the week, one bit each, Monday first. Held as a
// mask because it is stored as one, and because a set of seven is easier to
// reason about as a number than as a slice.
type DayMask uint8

// everyDayBits is the seven bits that mean something.
const everyDayBits DayMask = 1<<7 - 1

// dayNames are the API's spelling of each day, Monday first: the order a week
// is read in here, whatever time.Weekday's own order (Sunday first) says.
var dayNames = [7]string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// bitOf is the position of a weekday in the mask.
func bitOf(day time.Weekday) DayMask {
	return 1 << ((int(day) + 6) % 7)
}

// DaysOf builds a mask from weekdays. No weekday is the zero mask: every day.
func DaysOf(days ...time.Weekday) DayMask {
	var mask DayMask
	for _, day := range days {
		mask |= bitOf(day)
	}
	return mask
}

// ParseDays reads the API's day names. A repeat is not an error and the order
// does not matter; a name that is not one, or no name at all, is refused: to
// allow every day, leave the field out.
func ParseDays(names []string) (DayMask, error) {
	if len(names) == 0 {
		return 0, fmt.Errorf("%w: the window names no day; leave days out for every day", ErrInvalidEnvironment)
	}

	var mask DayMask
	for _, name := range names {
		index := -1
		for i, known := range dayNames {
			if name == known {
				index = i
			}
		}
		if index < 0 {
			return 0, fmt.Errorf("%w: %q is not a day (mon, tue, wed, thu, fri, sat, sun)", ErrInvalidEnvironment, name)
		}
		mask |= 1 << index
	}
	return mask, nil
}

// Names lists the days in week order, in the API's spelling. Empty for every
// day, which is how the API says it: by leaving the field out.
func (m DayMask) Names() []string {
	if m == 0 {
		return nil
	}

	names := make([]string, 0, 7)
	for i, name := range dayNames {
		if m&(1<<i) != 0 {
			names = append(names, name)
		}
	}
	return names
}

// Has reports whether day is in the set. The zero mask holds every day.
func (m DayMask) Has(day time.Weekday) bool {
	return m == 0 || m&bitOf(day) != 0
}

// String describes the set the way a person writes it: runs of consecutive days
// as a range, the rest separated by commas.
func (m DayMask) String() string {
	if m == 0 || m == everyDayBits {
		return "every day"
	}

	var parts []string
	for i := 0; i < 7; {
		if m&(1<<i) == 0 {
			i++
			continue
		}
		j := i
		for j+1 < 7 && m&(1<<(j+1)) != 0 {
			j++
		}
		if j == i {
			parts = append(parts, dayNames[i])
		} else {
			parts = append(parts, dayNames[i]+"–"+dayNames[j])
		}
		i = j + 1
	}

	// Sunday and Monday are neighbours in a week that goes round: "fri, sat,
	// sun, mon" is "fri–mon", the way somebody who asked for it would say it.
	if m&1 != 0 && m&(1<<6) != 0 && m != everyDayBits {
		return wrapped(m)
	}
	return strings.Join(parts, ", ")
}

// wrapped describes a set that includes both Sunday and Monday as one range
// across the end of the week, when it is one.
func wrapped(m DayMask) string {
	// Start at the first day after a gap, and walk forward round the week.
	start := -1
	for i := 0; i < 7; i++ {
		if m&(1<<i) != 0 && m&(1<<((i+6)%7)) == 0 {
			start = i
			break
		}
	}
	if start < 0 {
		return "every day"
	}

	length := 0
	for i := 0; i < 7 && m&(1<<((start+i)%7)) != 0; i++ {
		length++
	}
	if bits := popcount(m); bits != length {
		// More than one run: say them one by one, in week order.
		return strings.Join(m.Names(), ", ")
	}

	end := (start + length - 1) % 7
	if length == 1 {
		return dayNames[start]
	}
	return dayNames[start] + "–" + dayNames[end]
}

func popcount(m DayMask) int {
	count := 0
	for ; m != 0; m &= m - 1 {
		count++
	}
	return count
}

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
	case w.Days&^everyDayBits != 0:
		return fmt.Errorf("%w: the window names a day this code does not know", ErrInvalidEnvironment)
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
		return minute >= w.StartMinute && minute < w.EndMinute && w.Days.Has(local.Weekday())
	}
	// The window crosses midnight: it is everything from the start to the end
	// of the day, plus everything from the start of the day to the end. The
	// evening is part of the night that starts today; the morning is the end of
	// the one that started yesterday, so it is yesterday's day that must be
	// allowed.
	if minute >= w.StartMinute {
		return w.Days.Has(local.Weekday())
	}
	return minute < w.EndMinute && w.Days.Has(local.AddDate(0, 0, -1).Weekday())
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
		return fmt.Errorf("%w: it is open from %s to %s%s in %s",
			ErrOutsideAllowedWindow,
			clock(e.Window.StartMinute), clock(e.Window.EndMinute), onDays(e.Window.Days), e.Window.Zone)
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

// onDays is the part of a refusal that names the days, and nothing when the
// window is open on all of them.
func onDays(days DayMask) string {
	if days == 0 || days == everyDayBits {
		return ""
	}
	return " on " + days.String()
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
