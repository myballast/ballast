package dto

import (
	"bytes"
	"encoding/json"
	"time"
)

// Environment is a target a project may send load to.
//
// There is no field for a secret anywhere in this file beyond a key and its
// dates. A value is written to the API and never read back out of it.
type Environment struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	// VerificationStatus is one of pending, proof_ok, verified,
	// verification_failed. Only a verified target may be sent load.
	VerificationStatus string `json:"verification_status"`
	MaxVUs             int    `json:"max_vus"`
	MaxDurationS       int    `json:"max_duration_s"`
	// EffectiveMaxVUs and EffectiveMaxDurationS are what a run can really use
	// here: the smaller of what is declared above and the ceilings no
	// configuration can raise. A profile asking for more is lowered to these.
	EffectiveMaxVUs       int `json:"effective_max_vus"`
	EffectiveMaxDurationS int `json:"effective_max_duration_s"`
	// AllowedWindow is absent when the target takes load at any hour.
	AllowedWindow *AllowedWindow `json:"allowed_window,omitempty"`
	// VerificationCheckedAt is when somebody last asked Ballast to fetch the
	// challenge, successful or not. Absent until the first attempt.
	VerificationCheckedAt *time.Time `json:"verification_checked_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
}

// AllowedWindow is the stretch of the day a target accepts load, in its own
// timezone. A start after an end crosses midnight: 22:00 to 06:00.
type AllowedWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
	// Timezone is an IANA name, such as Europe/Paris.
	Timezone string `json:"timezone"`
	// Days narrows the window to some days of the week, read in Timezone, as
	// mon, tue, wed, thu, fri, sat, sun. Left out, every day. A window that
	// crosses midnight belongs to the day it starts: mon–fri 22:00–04:00 is
	// five nights, and Friday's ends on Saturday morning.
	Days []string `json:"days,omitempty"`
}

// EnvironmentListResponse is returned by the environment list.
type EnvironmentListResponse struct {
	Environments []Environment `json:"environments"`
}

// CreateEnvironmentRequest declares a target. It arrives unverified whatever the
// caller sends: nothing here can make an environment verified.
type CreateEnvironmentRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	// MaxVUs and MaxDurationS are the ceilings every run here is clamped to.
	MaxVUs       int `json:"max_vus"`
	MaxDurationS int `json:"max_duration_s"`
	// AllowedWindow is optional. Left out, the target takes load at any hour.
	AllowedWindow *AllowedWindow `json:"allowed_window,omitempty"`
}

// UpdateEnvironmentRequest changes a target. Anything left out stays as it is,
// and allowed_window sent as null removes the window.
//
// Pointing the environment at another origin resets its verification, because the
// proof was served by the old one.
type UpdateEnvironmentRequest struct {
	Name         *string `json:"name,omitempty"`
	BaseURL      *string `json:"base_url,omitempty"`
	MaxVUs       *int    `json:"max_vus,omitempty"`
	MaxDurationS *int    `json:"max_duration_s,omitempty"`
	// AllowedWindow is its own type because "leave it alone" and "there is no
	// window any more" are different instructions, and JSON spells the first as
	// an absent field and the second as null.
	AllowedWindow OptionalWindow `json:"allowed_window"`
}

// OptionalWindow is a window an update may leave alone, replace or remove.
//
// A double pointer cannot carry this: encoding/json decodes null into a nil
// outer pointer, which is exactly what an absent field leaves, so null would
// read as "leave it alone" and a window could never be removed. A type with its
// own UnmarshalJSON is called for null as for anything else, and not at all for
// an absent field.
type OptionalWindow struct {
	// Set is true when the field was in the body, as a window or as null.
	Set bool
	// Window is nil when the body said null.
	Window *AllowedWindow
}

// UnmarshalJSON records that the field was present, and what it held.
func (o *OptionalWindow) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(bytes.TrimSpace(data)) == "null" {
		o.Window = nil
		return nil
	}
	return json.Unmarshal(data, &o.Window)
}

// Secret is what Ballast will say about a stored credential: its key, and when it
// was set. The value is not returned by any endpoint.
type Secret struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SecretListResponse is returned by the secret list: the keys, never the values.
type SecretListResponse struct {
	Secrets []Secret `json:"secrets"`
}

// SetSecretRequest carries the one value that travels into Ballast and never
// back out. The key is in the path.
type SetSecretRequest struct {
	Value string `json:"value"`
}

// VerificationChallenge is what a target has to serve for Ballast to believe
// somebody owns it.
//
// The token is not a credential — it is meant to be published, on the target's
// own web server — but it is unguessable, so that nobody can serve a file in
// advance for an environment they do not own. It is returned here and nowhere
// else: the environment's own response does not carry it.
type VerificationChallenge struct {
	// Status is where the target stands: pending, proof_ok, verified or
	// verification_failed. Only verified lets load through.
	Status string `json:"verification_status"`
	Token  string `json:"token"`
	// URL is exactly where Ballast will look, and the file must hold the token.
	// /.well-known sits at the root of the origin, so a base URL with a path
	// does not change it.
	URL string `json:"url"`
	// VerifiedAt and VerifiedBy are absent until an administrator approves.
	// VerifiedByName is who that was, as the screen should say it: the name they
	// signed in with, or their address when they gave none.
	VerifiedAt     *time.Time `json:"verified_at,omitempty"`
	VerifiedBy     string     `json:"verified_by,omitempty"`
	VerifiedByName string     `json:"verified_by_name,omitempty"`
	// CheckedAt is when the challenge was last fetched, whatever came of it.
	CheckedAt *time.Time `json:"checked_at,omitempty"`
}

// VerificationFailed is the body of a 422 from the verify endpoint: the
// sentence, and what it was about.
type VerificationFailed struct {
	Error   string       `json:"error"`
	Failure ProofFailure `json:"failure"`
}

// ProofFailure says where Ballast looked for the challenge, what answered, and
// how it differed. It never carries what the target's file said: only its status
// and its size.
//
// The searched address contains the token, which is meant to be published but is
// read only by whoever manages environments; the endpoint takes that permission.
type ProofFailure struct {
	// Searched is the exact address the challenge was fetched from.
	Searched string `json:"searched"`
	// Status is the HTTP status the target answered. Absent when it answered
	// nothing: it could not be reached, resolved somewhere Ballast refuses, or
	// redirected away.
	Status int `json:"status,omitempty"`
	// Bytes is the size of the file it served, when one was read.
	Bytes int `json:"bytes,omitempty"`
	// TooLong reports a file longer than Ballast reads.
	TooLong bool `json:"too_long,omitempty"`
	// Reason is Ballast's own sentence for what went wrong.
	Reason string `json:"reason"`
}
