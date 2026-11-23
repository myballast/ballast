package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// A project's webhook is how a tool that is neither GitHub nor GitLab starts a run: it
// signs a request with a secret the project holds, and Ballast checks the signature.
//
// What a request has to prove is that its sender holds the secret, that it was made
// now, and that it is not one that was already served. And nothing in a refusal tells
// the sender which of those failed: that is what a guess is made of.

// WebhookSecretPrefix starts every signing secret. It is not the prefix of an API
// token, so that a scanner told what one looks like does not read the other as it.
const WebhookSecretPrefix = "blw_"

const (
	// webhookSecretBytes is how much randomness a secret carries: 256 bits.
	webhookSecretBytes = 32

	// WebhookSignatureVersion is written before the signature, so that the scheme can
	// change one day without a request being read as the wrong one.
	WebhookSignatureVersion = "v1"

	// WebhookTimestampWindow is how far from now a request's timestamp may be, on each
	// side: a sender's clock is never exactly Ballast's.
	WebhookTimestampWindow = 5 * time.Minute

	// WebhookDeliveryRetention is how long a delivery identifier is remembered. A
	// request is acceptable from one window before its timestamp to one after it, so
	// the same request can be sent again for twice the window: forgetting sooner opens
	// a gap.
	WebhookDeliveryRetention = 2 * WebhookTimestampWindow

	// MaxWebhookBody is the longest body that is read. The body is a few fields; this
	// bounds what an unauthenticated caller can make the server hold before the
	// signature has been looked at.
	MaxWebhookBody = 64 << 10

	webhookDeliveryMin = 16
	webhookDeliveryMax = 64
	// webhookTimestampMax is the digits of a Unix time in seconds until the year 2286.
	webhookTimestampMax = 10
)

// ErrWebhookRefused is every refusal of a webhook request, and the only sentence a
// sender is ever told. WebhookRefusalReason says which one it was, for whoever reads
// the log.
var ErrWebhookRefused = errors.New("the webhook request was refused")

// ErrInvalidWebhookBody reports a signed request whose body is not what a run is asked
// for. The signature was good, so the sender is who they say: what is wrong is the
// request, and they are told so, in this code's words and never in the body's.
var ErrInvalidWebhookBody = errors.New("the webhook request body is not valid")

// ErrWebhookExists reports a project that already has a webhook. A conflict with what
// exists, not a mistake in the request: replacing the secret is a rotation.
var ErrWebhookExists = errors.New("the project already has a webhook")

// WebhookRefusal is why a request was refused. It is for the operator's log and for
// nobody else.
type WebhookRefusal string

const (
	// WebhookMalformed is a header that is missing or is not what was specified.
	WebhookMalformed WebhookRefusal = "malformed"
	// WebhookStale is a timestamp that is outside the window.
	WebhookStale WebhookRefusal = "stale"
	// WebhookBadSignature is a signature that is not the one of this secret and this request.
	WebhookBadSignature WebhookRefusal = "bad_signature"
	// WebhookTooLarge is a body over the limit.
	WebhookTooLarge WebhookRefusal = "too_large"
	// WebhookReplayed is a delivery that was already served.
	WebhookReplayed WebhookRefusal = "replayed"
	// WebhookUnknown is when there is no webhook for this project, or none that can be read.
	WebhookUnknown WebhookRefusal = "unknown"
)

// refusal is ErrWebhookRefused with a reason that does not show in what it says.
type refusal struct{ reason WebhookRefusal }

func (refusal) Error() string            { return ErrWebhookRefused.Error() }
func (refusal) Is(target error) bool     { return target == ErrWebhookRefused }
func (r refusal) Reason() WebhookRefusal { return r.reason }
func refuse(reason WebhookRefusal) error { return refusal{reason: reason} }

// NewWebhookRefusal is a refusal for a reason that is found outside the signature: a
// replay, or a project with no webhook.
func NewWebhookRefusal(reason WebhookRefusal) error { return refuse(reason) }

// WebhookRefusalReason is why err was a refusal, or empty when it is not one.
func WebhookRefusalReason(err error) WebhookRefusal {
	var r refusal
	if errors.As(err, &r) {
		return r.reason
	}
	return ""
}

// Webhook is what is known of a project's webhook. It holds no secret: the signing
// secret is stored encrypted, shown once to whoever made it, and never returned.
type Webhook struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	// CreatedBy is the person who made it, nil once they have left.
	CreatedBy *uuid.UUID
	CreatedAt time.Time
	// RotatedAt is the last time the secret was replaced, nil if it never was.
	RotatedAt *time.Time
}

// NewWebhookSecret draws a secret from source, which is the operating system's
// randomness in production. A source that gives fewer bytes than asked is an error:
// a secret made mostly of zeros is a guessable one.
func NewWebhookSecret(source io.Reader) (string, error) {
	raw := make([]byte, webhookSecretBytes)
	if _, err := io.ReadFull(source, raw); err != nil {
		return "", fmt.Errorf("read randomness for a webhook secret: %w", err)
	}
	return WebhookSecretPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// SignWebhook is the signature of a request: the HMAC-SHA256, keyed with the secret,
// of "<timestamp>.<delivery>.<body>", written "v1=<hex>".
//
// The delivery is part of what is signed. Left out, a captured request could be sent
// again under a new identifier, and the check for replays would stop honest retries
// and nothing else.
func SignWebhook(secret string, timestamp int64, delivery string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte{'.'})
	mac.Write([]byte(delivery))
	mac.Write([]byte{'.'})
	mac.Write(body)
	return WebhookSignatureVersion + "=" + hex.EncodeToString(mac.Sum(nil))
}

// WebhookRequest is what a request says about itself, read from its headers.
type WebhookRequest struct {
	Timestamp  int64
	DeliveryID string
	// signature is the header as it came, exactly "v1=" and 64 lower-case hex digits.
	signature string
}

// ParseWebhookRequest reads the three headers, and takes nothing that is not exactly
// what was specified: no sign, no space, no other version, no second signature.
func ParseWebhookRequest(timestamp, delivery, signature string) (WebhookRequest, error) {
	stamp, ok := parseUnixSeconds(timestamp)
	if !ok || !validDelivery(delivery) || !validSignature(signature) {
		return WebhookRequest{}, refuse(WebhookMalformed)
	}
	return WebhookRequest{Timestamp: stamp, DeliveryID: delivery, signature: signature}, nil
}

func parseUnixSeconds(s string) (int64, bool) {
	if s == "" || len(s) > webhookTimestampMax {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

func validDelivery(s string) bool {
	if len(s) < webhookDeliveryMin || len(s) > webhookDeliveryMax {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}

func validSignature(s string) bool {
	prefix := WebhookSignatureVersion + "="
	if len(s) != len(prefix)+sha256.Size*2 || s[:len(prefix)] != prefix {
		return false
	}
	for i := len(prefix); i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Verify says whether the request was signed with secret, over this body, and made
// within the window of now. It does not know whether the delivery was already served:
// that is what the database says, in the transaction that records it.
//
// The size is looked at first and the signature is compared in constant time. An
// empty secret verifies nothing: HMAC with an empty key is a valid HMAC, and a project
// with no secret must not accept what anybody can sign.
func (r WebhookRequest) Verify(secret string, body []byte, now time.Time) error {
	if len(body) > MaxWebhookBody {
		return refuse(WebhookTooLarge)
	}
	if secret == "" {
		return refuse(WebhookUnknown)
	}

	delta := now.Sub(time.Unix(r.Timestamp, 0))
	if delta < -WebhookTimestampWindow || delta > WebhookTimestampWindow {
		return refuse(WebhookStale)
	}

	want := SignWebhook(secret, r.Timestamp, r.DeliveryID, body)
	if !hmac.Equal([]byte(want), []byte(r.signature)) {
		return refuse(WebhookBadSignature)
	}
	return nil
}

// WebhookPermissions is everything a webhook may do, in its own project: start a run.
// Fixed here, and not read from anywhere. Not even to read: a webhook has nobody to
// show anything to, and a permission it does not need is one it must not hold.
func WebhookPermissions() PermissionSet {
	return PermissionSet(PermRunTest)
}

// Allows reports whether the webhook may do perm in projectID. perm may name several
// permissions, and every one of them has to be held.
func (w Webhook) Allows(perm Permission, projectID uuid.UUID) bool {
	if perm == 0 || projectID == uuid.Nil || projectID != w.ProjectID {
		return false
	}
	return PermissionSet(perm)&^WebhookPermissions() == 0
}

// WebhookCreatedEntry is the journal's line for a webhook that was made. It carries no
// detail at all: the webhook's own row says the rest, and the secret has no way in.
func WebhookCreatedEntry(actor *uuid.UUID, hook Webhook, at time.Time) AuditEntry {
	return webhookEntry(actor, hook, ActionWebhookCreated, at)
}

// WebhookRotatedEntry is the journal's line for a webhook whose secret was replaced.
func WebhookRotatedEntry(actor *uuid.UUID, hook Webhook, at time.Time) AuditEntry {
	return webhookEntry(actor, hook, ActionWebhookRotated, at)
}

// WebhookDeletedEntry is the journal's line for a webhook that was removed.
func WebhookDeletedEntry(actor *uuid.UUID, hook Webhook, at time.Time) AuditEntry {
	return webhookEntry(actor, hook, ActionWebhookDeleted, at)
}

func webhookEntry(actor *uuid.UUID, hook Webhook, action AuditAction, at time.Time) AuditEntry {
	return Allow(hook.OrgID, actor, action, TargetWebhook, at).InProject(hook.ProjectID).On(hook.ID)
}
