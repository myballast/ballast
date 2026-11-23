package domain

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// A project's webhook lets a tool that is neither GitHub nor GitLab start a run by
// signing a request. What a request has to prove is that whoever sent it holds the
// project's secret, that it was made now, and that it is not one that was already
// served. These tests defend those three, and one more: that nothing about a refusal
// tells the sender which of them failed, because that is what a guess is made of.
//
// Written before the code, which is why they do not compile yet.

const (
	aWebhookSecret   = "blw_test-secret"
	aDelivery        = "delivery-0123456789"
	aWebhookBody     = `{"scenario_id":"x"}`
	aWebhookStamp    = int64(1700000000)
	aKnownSignature  = "v1=5fd538da6c3431bf2a76fc1b2822d9fa39eba0b299d6533463853697a9b51223"
	aWebhookNowStamp = time.Duration(aWebhookStamp) * time.Second
)

func theMoment() time.Time { return time.Unix(aWebhookStamp, 0).UTC() }

func TestTheSignatureIsTheHMACOfTheTimestampTheDeliveryAndTheBody(t *testing.T) {
	// The expected value was computed apart from this code, with Python's hmac, over
	// "<timestamp>.<delivery>.<body>": a test that computed it with the function under
	// test would pass whatever the function did.
	got := SignWebhook(aWebhookSecret, aWebhookStamp, aDelivery, []byte(aWebhookBody))
	if got != aKnownSignature {
		t.Errorf("signature %s, want %s", got, aKnownSignature)
	}
}

func TestEveryPartOfWhatIsSignedChangesTheSignature(t *testing.T) {
	// The delivery identifier is signed: otherwise a captured request could be sent
	// again under a new identifier, and the protection against replays would be a
	// protection against honest retries only.
	base := SignWebhook(aWebhookSecret, aWebhookStamp, aDelivery, []byte(aWebhookBody))
	for name, other := range map[string]string{
		"the secret":    SignWebhook("blw_other", aWebhookStamp, aDelivery, []byte(aWebhookBody)),
		"the timestamp": SignWebhook(aWebhookSecret, aWebhookStamp+1, aDelivery, []byte(aWebhookBody)),
		"the delivery":  SignWebhook(aWebhookSecret, aWebhookStamp, aDelivery+"x", []byte(aWebhookBody)),
		"the body":      SignWebhook(aWebhookSecret, aWebhookStamp, aDelivery, []byte(aWebhookBody+" ")),
	} {
		if other == base {
			t.Errorf("changing %s did not change the signature", name)
		}
	}
	// The parts are joined by a separator that no part can contain, so that two
	// different splits of the same text are not the same message.
	a := SignWebhook(aWebhookSecret, 1, "aaaaaaaaaaaaaaaa", []byte("1.x"))
	b := SignWebhook(aWebhookSecret, 11, "aaaaaaaaaaaaaaaa", []byte(".x"))
	if a == b {
		t.Error("two different requests have the same signature")
	}
}

func TestANewSecretCarriesADetectablePrefixAndALotOfRandomness(t *testing.T) {
	secret, err := NewWebhookSecret(bytes.NewReader(bytes.Repeat([]byte{0x41}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, WebhookSecretPrefix) || WebhookSecretPrefix != "blw_" {
		t.Errorf("secret %q does not start with blw_", secret)
	}
	// 32 random bytes in base64url without padding is 43 characters.
	if len(secret) != len(WebhookSecretPrefix)+43 {
		t.Errorf("secret is %d characters, want %d: 32 random bytes", len(secret), len(WebhookSecretPrefix)+43)
	}
	for _, c := range strings.TrimPrefix(secret, WebhookSecretPrefix) {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
		if !ok {
			t.Errorf("secret has %q, which is not URL-safe", c)
		}
	}
	// It is not the token prefix: a scanner told what a token looks like must not
	// read a signing secret as one, nor the other way round.
	if WebhookSecretPrefix == APITokenPrefix {
		t.Error("the secret and the API token share a prefix")
	}
}

func TestTwoSecretsAreNotTheSameAndAShortOrFailingSourceIsAnError(t *testing.T) {
	a, _ := NewWebhookSecret(bytes.NewReader(bytes.Repeat([]byte{1}, 32)))
	b, _ := NewWebhookSecret(bytes.NewReader(bytes.Repeat([]byte{2}, 32)))
	if a == b {
		t.Error("different randomness gave the same secret")
	}
	if _, err := NewWebhookSecret(bytes.NewReader([]byte{1, 2, 3})); err == nil {
		t.Error("a source that ran out of bytes gave a secret: it would have been mostly zeros")
	}
	if _, err := NewWebhookSecret(iotest.ErrReader(errors.New("no entropy"))); err == nil {
		t.Error("a failing source gave a secret")
	}
}

// --- Reading the headers -----------------------------------------------------------------------

func TestTheHeadersOfAWellFormedRequestAreRead(t *testing.T) {
	r, err := ParseWebhookRequest("1700000000", aDelivery, aKnownSignature)
	if err != nil {
		t.Fatal(err)
	}
	if r.Timestamp != aWebhookStamp || r.DeliveryID != aDelivery {
		t.Errorf("read %+v", r)
	}
}

func TestHeadersThatAreNotExactlyWhatWasSpecifiedAreRefused(t *testing.T) {
	good := strings.TrimPrefix(aKnownSignature, "v1=")
	cases := map[string][3]string{
		"no timestamp":               {"", aDelivery, aKnownSignature},
		"a timestamp with a sign":    {"+1700000000", aDelivery, aKnownSignature},
		"a negative timestamp":       {"-1", aDelivery, aKnownSignature},
		"a timestamp with a space":   {" 1700000000", aDelivery, aKnownSignature},
		"a timestamp that is a date": {"2023-11-14T22:13:20Z", aDelivery, aKnownSignature},
		"a timestamp in hex":         {"0x65", aDelivery, aKnownSignature},
		"a timestamp too long":       {"17000000000", aDelivery, aKnownSignature},
		"a decimal timestamp":        {"1700000000.5", aDelivery, aKnownSignature},
		"no delivery":                {"1700000000", "", aKnownSignature},
		"a delivery too short":       {"1700000000", "abc", aKnownSignature},
		"a delivery too long":        {"1700000000", strings.Repeat("a", 65), aKnownSignature},
		"a delivery with a space":    {"1700000000", "delivery 0123456789", aKnownSignature},
		"a delivery with a newline":  {"1700000000", "delivery-012345678\n", aKnownSignature},
		"a delivery with a slash":    {"1700000000", "delivery/0123456789", aKnownSignature},
		"no signature":               {"1700000000", aDelivery, ""},
		"no version":                 {"1700000000", aDelivery, good},
		"another version":            {"1700000000", aDelivery, "v2=" + good},
		"upper case hex":             {"1700000000", aDelivery, "v1=" + strings.ToUpper(good)},
		"a short signature":          {"1700000000", aDelivery, "v1=" + good[:62]},
		"a long signature":           {"1700000000", aDelivery, "v1=" + good + "00"},
		"not hex":                    {"1700000000", aDelivery, "v1=" + strings.Repeat("z", 64)},
		"two signatures":             {"1700000000", aDelivery, aKnownSignature + ",v1=" + good},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseWebhookRequest(c[0], c[1], c[2]); !errors.Is(err, ErrWebhookRefused) {
				t.Errorf("accepted: %v", err)
			}
		})
	}
}

// --- Verifying ---------------------------------------------------------------------------------

func parsed(t *testing.T, stamp int64, delivery, secret, body string) WebhookRequest {
	t.Helper()
	signature := SignWebhook(secret, stamp, delivery, []byte(body))
	r, err := ParseWebhookRequest(itoa64(stamp), delivery, signature)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return r
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

func TestARequestSignedWithTheSecretAndMadeNowIsAccepted(t *testing.T) {
	r := parsed(t, aWebhookStamp, aDelivery, aWebhookSecret, aWebhookBody)
	if err := r.Verify(aWebhookSecret, []byte(aWebhookBody), theMoment()); err != nil {
		t.Errorf("refused: %v", err)
	}
}

func TestAWrongSignatureIsRefused(t *testing.T) {
	r := parsed(t, aWebhookStamp, aDelivery, "blw_not-the-secret", aWebhookBody)
	if err := r.Verify(aWebhookSecret, []byte(aWebhookBody), theMoment()); !errors.Is(err, ErrWebhookRefused) {
		t.Errorf("accepted: %v", err)
	}

	// A body that is not the one that was signed.
	r = parsed(t, aWebhookStamp, aDelivery, aWebhookSecret, aWebhookBody)
	if err := r.Verify(aWebhookSecret, []byte(aWebhookBody+" "), theMoment()); !errors.Is(err, ErrWebhookRefused) {
		t.Errorf("a changed body was accepted: %v", err)
	}
	// A delivery that is not the one that was signed.
	r.DeliveryID = aDelivery + "x"
	if err := r.Verify(aWebhookSecret, []byte(aWebhookBody), theMoment()); !errors.Is(err, ErrWebhookRefused) {
		t.Errorf("a changed delivery was accepted: %v", err)
	}
}

func TestAnEmptySecretNeverVerifiesAnything(t *testing.T) {
	// HMAC with an empty key is a valid HMAC. A project that has no secret must not
	// accept the request anybody can sign with one.
	r := parsed(t, aWebhookStamp, aDelivery, "", aWebhookBody)
	if err := r.Verify("", []byte(aWebhookBody), theMoment()); !errors.Is(err, ErrWebhookRefused) {
		t.Errorf("a request signed with no secret was accepted: %v", err)
	}
}

func TestATimestampOutsideTheWindowIsRefusedOnBothSides(t *testing.T) {
	window := WebhookTimestampWindow
	if window != 5*time.Minute {
		t.Fatalf("the window is %s, want 5m", window)
	}
	for name, offset := range map[string]time.Duration{
		"just inside, in the past":   -window,
		"just inside, in the future": window,
		"now":                        0,
	} {
		r := parsed(t, aWebhookStamp+int64(offset/time.Second), aDelivery, aWebhookSecret, aWebhookBody)
		if err := r.Verify(aWebhookSecret, []byte(aWebhookBody), theMoment()); err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
	}
	for name, offset := range map[string]time.Duration{
		"a second too old":   -window - time.Second,
		"a second too new":   window + time.Second,
		"a day ago":          -24 * time.Hour,
		"a day from now":     24 * time.Hour,
		"the start of epoch": -time.Duration(aWebhookStamp) * time.Second,
	} {
		r := parsed(t, aWebhookStamp+int64(offset/time.Second), aDelivery, aWebhookSecret, aWebhookBody)
		if err := r.Verify(aWebhookSecret, []byte(aWebhookBody), theMoment()); !errors.Is(err, ErrWebhookRefused) {
			t.Errorf("%s: accepted: %v", name, err)
		}
	}
}

func TestABodyLargerThanTheLimitIsRefusedWhateverItsSignature(t *testing.T) {
	big := strings.Repeat("a", MaxWebhookBody+1)
	r := parsed(t, aWebhookStamp, aDelivery, aWebhookSecret, big)
	if err := r.Verify(aWebhookSecret, []byte(big), theMoment()); !errors.Is(err, ErrWebhookRefused) {
		t.Errorf("accepted: %v", err)
	}
	ok := strings.Repeat("a", MaxWebhookBody)
	r = parsed(t, aWebhookStamp, aDelivery, aWebhookSecret, ok)
	if err := r.Verify(aWebhookSecret, []byte(ok), theMoment()); err != nil {
		t.Errorf("a body of exactly the limit was refused: %v", err)
	}
	if MaxWebhookBody != 64<<10 {
		t.Errorf("the body limit is %d, want 64 KiB", MaxWebhookBody)
	}
}

func TestAllRefusalsLookTheSameToTheSenderAndDifferToTheOperator(t *testing.T) {
	stale := parsed(t, aWebhookStamp-3600, aDelivery, aWebhookSecret, aWebhookBody)
	wrong := parsed(t, aWebhookStamp, aDelivery, "blw_other", aWebhookBody)

	e1 := stale.Verify(aWebhookSecret, []byte(aWebhookBody), theMoment())
	e2 := wrong.Verify(aWebhookSecret, []byte(aWebhookBody), theMoment())
	if e1 == nil || e2 == nil {
		t.Fatal("a request was accepted")
	}
	// What a sender is told is the one sentence, whatever failed.
	if e1.Error() != e2.Error() || e1.Error() != ErrWebhookRefused.Error() {
		t.Errorf("the refusals read %q and %q: they must be the same sentence", e1, e2)
	}
	// What the operator reads in the log is the reason, apart from the error.
	if WebhookRefusalReason(e1) != WebhookStale || WebhookRefusalReason(e2) != WebhookBadSignature {
		t.Errorf("reasons %q and %q", WebhookRefusalReason(e1), WebhookRefusalReason(e2))
	}
	if WebhookRefusalReason(nil) != "" || WebhookRefusalReason(errors.New("x")) != "" {
		t.Error("a reason was given for something that is not a refusal")
	}
}

func TestNoRefusalCarriesTheSecretOrTheSignature(t *testing.T) {
	r := parsed(t, aWebhookStamp, aDelivery, "blw_other", aWebhookBody)
	err := r.Verify(aWebhookSecret, []byte(aWebhookBody), theMoment())
	for _, leak := range []string{aWebhookSecret, "blw_other", aKnownSignature, r.DeliveryID} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the refusal carries %q", leak)
		}
	}
}

// --- What is kept of a webhook -----------------------------------------------------------------

func TestAWebhookHasNoFieldThatCouldHoldTheSecret(t *testing.T) {
	// The guarantee is structural, as it is for an environment's secrets: a value that
	// has nowhere to live in the type cannot be returned by a handler that forgot.
	typ := reflect.TypeOf(Webhook{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.ToLower(typ.Field(i).Name)
		for _, bad := range []string{"secret", "key", "value", "hash", "cipher", "encrypted"} {
			if strings.Contains(name, bad) {
				t.Errorf("Webhook has a field %q", typ.Field(i).Name)
			}
		}
	}
}

func TestADeliveryIsKeptForAsLongAsAnAcceptableRequestCouldBeReplayed(t *testing.T) {
	// A request is acceptable from five minutes before its timestamp to five minutes
	// after it, so the same request can be sent again for up to ten. Forgetting a
	// delivery sooner than that opens a window; the retention must cover both sides.
	if WebhookDeliveryRetention < 2*WebhookTimestampWindow {
		t.Errorf("deliveries are kept %s, the window is %s on each side", WebhookDeliveryRetention, WebhookTimestampWindow)
	}
}
