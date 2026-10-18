// The tests in this file are written against an implementation that does not
// exist yet. They are the specification of it: this is a critical zone, and the
// verifier fetches a URL somebody else chose, from inside Ballast's own network.
//
// An internal test package on purpose. The constructor the production code
// exposes builds its own guarded client and takes no transport, so that nothing
// outside this package can hand it an unguarded one. The tests reach the
// unexported constructor instead, the way runner/hosted tests reach the entry
// template.
package verifier

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// token is what the target is expected to serve. 32 random bytes in base64url,
// as crypto.RandomTokens produces.
const token = "Zm9yLXRlc3Rpbmctb25seS1ub3QtYS1yZWFsLXRva2Vu"

// stubTransport answers without a socket, which is the only way to exercise the
// happy path: every address a test server can listen on is a loopback address,
// and the guard refuses those by design.
type stubTransport struct {
	answer func(*http.Request) (*http.Response, error)
	seen   []*http.Request
}

func (s *stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	s.seen = append(s.seen, r)
	return s.answer(r)
}

func answering(status int, body string) *stubTransport {
	return &stubTransport{answer: func(*http.Request) (*http.Response, error) {
		return response(status, body), nil
	}}
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func redirectTo(location string) *http.Response {
	res := response(http.StatusFound, "")
	res.Header.Set("Location", location)
	return res
}

// stubbed builds a verifier that answers through rt, with a short timeout so a
// test of the timeout does not take five seconds.
func stubbed(rt http.RoundTripper) *Verifier {
	return newWithTransport(rt, 200*time.Millisecond)
}

const proofURL = "https://staging.example.test/.well-known/ballast-" + token

// ---------------------------------------------------------------------------
// The address guard, and the reason it sits on the dialer rather than in front
// of the request: the address checked is the one the kernel is about to connect
// to, so a DNS answer that changes between the check and the connection has
// nothing left to change.
// ---------------------------------------------------------------------------

// The address table that used to be here moved to infra/netguard, where the
// predicate now lives: the egress proxy asks the same question, and one of the
// two copies would have gone stale. What stays here is the half netguard cannot
// answer — that the predicate is wired to the dialer at all.

// The guard, reached the way it will be in production: through a real socket.
// A test server listens on the loopback, so pointing the verifier at it must
// fail — and fail with the address refusal rather than with a missing file,
// because the connection must never be made at all.
func TestARealRequestToTheLoopbackIsRefused(t *testing.T) {
	t.Parallel()

	var reached bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		_, _ = w.Write([]byte(token))
	}))
	t.Cleanup(server.Close)

	err := New().ReadProof(context.Background(), server.URL+"/.well-known/ballast-"+token, token)

	if !errors.Is(err, ErrAddressRefused) {
		t.Fatalf("got %v, want ErrAddressRefused", err)
	}
	if reached {
		t.Fatal("the request arrived; the guard let the connection through")
	}
}

// The same thing behind a name rather than an address literal. This is what
// proves the check happens after DNS resolution: "localhost" is not an address,
// and a guard written as a string comparison on the URL would wave it past.
func TestAHostnameThatResolvesToTheLoopbackIsRefused(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(token))
	}))
	t.Cleanup(server.Close)

	// http://127.0.0.1:PORT -> http://localhost:PORT
	byName := strings.Replace(server.URL, "127.0.0.1", "localhost", 1)

	err := New().ReadProof(context.Background(), byName+"/.well-known/ballast-"+token, token)
	if !errors.Is(err, ErrAddressRefused) {
		t.Fatalf("got %v, want ErrAddressRefused", err)
	}
}

// ---------------------------------------------------------------------------
// Redirects
// ---------------------------------------------------------------------------

func TestARedirectToAnotherHostIsRefused(t *testing.T) {
	t.Parallel()

	elsewhere := map[string]string{
		"a private host":          "http://10.0.0.1/.well-known/ballast-" + token,
		"the metadata service":    "http://169.254.169.254/latest/meta-data/",
		"the loopback":            "http://127.0.0.1:9000/",
		"another public host":     "https://not-ours.example.test/.well-known/ballast-" + token,
		"another port, same host": "https://staging.example.test:8443/.well-known/ballast-" + token,
	}

	for name, location := range elsewhere {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			hops := 0
			rt := &stubTransport{answer: func(*http.Request) (*http.Response, error) {
				hops++
				if hops == 1 {
					return redirectTo(location), nil
				}
				// Reaching here means the redirect was followed.
				return response(http.StatusOK, token), nil
			}}

			err := stubbed(rt).ReadProof(context.Background(), proofURL, token)
			if !errors.Is(err, ErrRedirected) {
				t.Fatalf("a redirect to %s answered %v, want ErrRedirected", name, err)
			}
			if hops > 1 {
				t.Fatalf("the redirect to %s was followed", name)
			}
		})
	}
}

// http to https on the same host is how a great many sites are configured, and
// refusing it would make the feature unusable for them. The address guard runs
// again on the second connection, so a same-host redirect is not a way around it.
func TestARedirectToTheSameHostIsFollowed(t *testing.T) {
	t.Parallel()

	hops := 0
	rt := &stubTransport{answer: func(*http.Request) (*http.Response, error) {
		hops++
		if hops == 1 {
			return redirectTo("https://staging.example.test/.well-known/ballast-" + token), nil
		}
		return response(http.StatusOK, token), nil
	}}

	if err := stubbed(rt).ReadProof(context.Background(),
		"http://staging.example.test/.well-known/ballast-"+token, token); err != nil {
		t.Fatalf("a redirect from http to https on the same host was refused: %v", err)
	}
	if hops != 2 {
		t.Fatalf("the request was made %d times", hops)
	}
}

func TestARedirectLoopOnTheSameHostStops(t *testing.T) {
	t.Parallel()

	hops := 0
	rt := &stubTransport{answer: func(*http.Request) (*http.Response, error) {
		hops++
		return redirectTo(proofURL), nil
	}}

	if err := stubbed(rt).ReadProof(context.Background(), proofURL, token); err == nil {
		t.Fatal("an endless redirect was followed to the end of time")
	}
	if hops > 4 {
		t.Fatalf("the verifier made %d requests following a loop", hops)
	}
}

// ---------------------------------------------------------------------------
// What counts as a proof
// ---------------------------------------------------------------------------

func TestAServedTokenIsAProof(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"exactly the token":    token,
		"with a newline":       token + "\n",
		"with trailing spaces": "  " + token + "  \n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := stubbed(answering(http.StatusOK, body)).
				ReadProof(context.Background(), proofURL, token); err != nil {
				t.Fatalf("a file holding %q was refused: %v", body, err)
			}
		})
	}
}

// The first acceptance criterion: no file, no proof.
func TestAMissingProofIsNotAProof(t *testing.T) {
	t.Parallel()

	for name, status := range map[string]int{
		"not found":    http.StatusNotFound,
		"forbidden":    http.StatusForbidden,
		"server error": http.StatusInternalServerError,
		"anything odd": http.StatusTeapot,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := stubbed(answering(status, "")).ReadProof(context.Background(), proofURL, token)
			if !errors.Is(err, ErrProofMissing) {
				t.Fatalf("%s answered %v, want ErrProofMissing", name, err)
			}
		})
	}
}

// A great many servers answer 200 to everything: a single-page application
// serving its index.html on every unknown path, a custom error page. Checking
// only the status code would accept all of them as proof of ownership, which
// would make the whole verification worthless.
func TestATwoHundredThatIsNotTheTokenIsNotAProof(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"an empty file":     "",
		"somebody else's":   "Zm9yLXRlc3Rpbmctb25seS1hbm90aGVyLXRva2VuLWVudGly",
		"an index page":     "<!doctype html><title>Staging</title>",
		"the token buried":  "<html>" + token + "</html>",
		"half of the token": token[:20],
		"the token twice":   token + token,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := stubbed(answering(http.StatusOK, body)).ReadProof(context.Background(), proofURL, token)
			if !errors.Is(err, ErrProofMismatch) {
				t.Fatalf("%s answered %v, want ErrProofMismatch", name, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Bounds
// ---------------------------------------------------------------------------

// countingReader is an endless body that reports how much of it was read.
type countingReader struct{ read int }

func (c *countingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	c.read += len(p)
	return len(p), nil
}

func (c *countingReader) Close() error { return nil }

// The body is a user-controlled host's answer. Without a bound, a target that
// streams for ever takes the worker's memory with it.
func TestTheBodyIsNotReadBeyondItsBound(t *testing.T) {
	t.Parallel()

	endless := &countingReader{}
	rt := &stubTransport{answer: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: endless, Header: make(http.Header)}, nil
	}}

	if err := stubbed(rt).ReadProof(context.Background(), proofURL, token); err == nil {
		t.Fatal("an endless body was accepted as a proof")
	}
	// A generous ceiling: what matters is that it stopped, not the exact figure.
	if endless.read > 1<<16 {
		t.Fatalf("%d bytes were read from an endless body", endless.read)
	}
}

func TestASlowTargetDoesNotHoldTheVerifierOpen(t *testing.T) {
	t.Parallel()

	rt := &stubTransport{answer: func(r *http.Request) (*http.Response, error) {
		select {
		case <-time.After(5 * time.Second):
			return response(http.StatusOK, token), nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}}

	started := time.Now()
	err := stubbed(rt).ReadProof(context.Background(), proofURL, token)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("got %v, want ErrUnreachable", err)
	}
	if waited := time.Since(started); waited > 2*time.Second {
		t.Fatalf("the verifier waited %s", waited)
	}
}

func TestACancelledContextStopsTheRequest(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := stubbed(answering(http.StatusOK, token)).ReadProof(ctx, proofURL, token)
	if err == nil {
		t.Fatal("a cancelled verification went ahead")
	}
}

// ---------------------------------------------------------------------------
// What is sent
// ---------------------------------------------------------------------------

// Only the two schemes, checked here as well as in the environment's own
// validation: this function is handed a URL and must not trust where it came
// from.
func TestOnlyHTTPAndHTTPSAreFetched(t *testing.T) {
	t.Parallel()

	for _, url := range []string{
		"file:///etc/passwd",
		"ftp://staging.example.test/token",
		"gopher://staging.example.test/",
		"staging.example.test/token",
		"",
	} {
		err := stubbed(answering(http.StatusOK, token)).ReadProof(context.Background(), url, token)
		if err == nil {
			t.Fatalf("%q was fetched", url)
		}
	}
}

// The request goes to a host somebody else controls, so it carries nothing of
// ours beyond a name they can recognise in their access log.
func TestTheRequestCarriesNothingOfOurs(t *testing.T) {
	t.Parallel()

	rt := answering(http.StatusOK, token)
	if err := stubbed(rt).ReadProof(context.Background(), proofURL, token); err != nil {
		t.Fatalf("ReadProof: %v", err)
	}

	if len(rt.seen) != 1 {
		t.Fatalf("%d requests were made", len(rt.seen))
	}
	sent := rt.seen[0]

	if sent.Method != http.MethodGet {
		t.Errorf("the proof was fetched with %s", sent.Method)
	}
	for _, header := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
		if sent.Header.Get(header) != "" {
			t.Errorf("the request carries a %s header", header)
		}
	}
	if agent := sent.Header.Get("User-Agent"); !strings.Contains(strings.ToLower(agent), "ballast") {
		t.Errorf("the user agent is %q; it should say who is calling", agent)
	}
}

// ---------------------------------------------------------------------------
// What a failure says. The person who declared the target needs three things:
// where Ballast looked, what answered, and how it differed. Not what the page
// said: the verifier reads a URL somebody chose, and repeating a body back would
// turn the endpoint into a way to read pages through Ballast.
// ---------------------------------------------------------------------------

func observed(t *testing.T, err error) *domain.ProofObserved {
	t.Helper()

	var seen *domain.ProofObserved
	if !errors.As(err, &seen) {
		t.Fatalf("the failure carries no observation: %v", err)
	}
	return seen
}

func TestAMissingProofSaysWhatTheTargetAnswered(t *testing.T) {
	t.Parallel()

	err := stubbed(answering(http.StatusNotFound, "no such file")).ReadProof(context.Background(), proofURL, token)

	if seen := observed(t, err); seen.Status != http.StatusNotFound {
		t.Errorf("observed status %d, want 404", seen.Status)
	}
	if !errors.Is(err, ErrProofMissing) {
		t.Errorf("the sentinel is lost: %v", err)
	}
}

func TestAMismatchSaysTheStatusAndTheSizeAndNotTheContent(t *testing.T) {
	t.Parallel()

	page := "<html><body>PRIVATE-DASHBOARD-CONTENT-9d41</body></html>"
	err := stubbed(answering(http.StatusOK, page)).ReadProof(context.Background(), proofURL, token)

	seen := observed(t, err)
	if seen.Status != http.StatusOK || seen.Bytes != len(page) {
		t.Errorf("observed %d / %d bytes, want 200 / %d", seen.Status, seen.Bytes, len(page))
	}
	if !errors.Is(err, ErrProofMismatch) {
		t.Errorf("the sentinel is lost: %v", err)
	}
	if strings.Contains(err.Error(), "PRIVATE-DASHBOARD-CONTENT") {
		t.Errorf("the failure repeats the target's content: %q", err)
	}
}

// The observation is a status and a size and has nowhere to put anything else.
// Pinned by shape: a field added to carry "a little of the body" would be the
// whole leak.
func TestAnObservationHasNowhereToHoldContent(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(domain.ProofObserved{})
	allowed := map[string]bool{"Status": true, "Bytes": true, "Truncated": true, "Err": true}
	for i := 0; i < typ.NumField(); i++ {
		if name := typ.Field(i).Name; !allowed[name] {
			t.Errorf("ProofObserved has a field %q; only %v are allowed", name, allowed)
		}
	}
}

func TestAFileThatIsTooLongSaysSoWithoutItsSize(t *testing.T) {
	t.Parallel()

	err := stubbed(answering(http.StatusOK, strings.Repeat("x", maxProofBytes+500))).
		ReadProof(context.Background(), proofURL, token)

	seen := observed(t, err)
	if !seen.Truncated || seen.Status != http.StatusOK {
		t.Errorf("observed %+v, want a truncated 200", seen)
	}
}

func TestATargetThatAnsweredNothingObservesNoStatus(t *testing.T) {
	t.Parallel()

	refused := &stubTransport{answer: func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	}}
	err := stubbed(refused).ReadProof(context.Background(), proofURL, token)

	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("got %v", err)
	}
	var seen *domain.ProofObserved
	if errors.As(err, &seen) && seen.Status != 0 {
		t.Errorf("a request that got no answer observed status %d", seen.Status)
	}
}
