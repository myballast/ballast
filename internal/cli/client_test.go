package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The command line tool is the one piece of Ballast that runs on somebody else's
// machine, with a token in its environment, printing into a log other people read.
// What these tests hold in place is the client under it:
//
//   - it sends the token the one way it should be sent and never prints it, whatever
//     the server says back;
//   - it refuses a configuration that would send the token somewhere unsafe, and
//     says which variable to change without repeating what was set;
//   - it never follows a redirect, and never repeats a request that may have started
//     a run;
//   - what the server says is printed as the server said it, made safe to print.
//
// Written before the code, which is why they do not compile yet.

const aToken = "blt_" + "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCdE"

func clientFor(t *testing.T, handler http.HandlerFunc, opts ...Option) (*Client, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL, aToken, opts...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, server
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// --- What the client agrees to be configured with --------------------------------------

func TestTheClientRefusesAnAddressThatWouldSendTheTokenSomewhereUnsafe(t *testing.T) {
	cases := []struct {
		name, address string
		accepted      bool
	}{
		{"https", "https://ballast.example.test", true},
		{"https under a prefix", "https://example.test/ballast", true},
		{"a trailing slash", "https://ballast.example.test/", true},
		// Plain http carries the token in the clear, which is acceptable only when
		// it never leaves the machine.
		{"http on localhost", "http://localhost:8080", true},
		{"http on the loopback address", "http://127.0.0.1:8080", true},
		{"http on the IPv6 loopback address", "http://[::1]:8080", true},
		{"http on a .localhost name", "http://api.localhost:8080", true},
		{"http elsewhere", "http://ballast.example.test", false},
		{"http on a private address", "http://10.0.0.5:8080", false},
		{"no address", "", false},
		{"no scheme", "ballast.example.test", false},
		{"another scheme", "ftp://ballast.example.test", false},
		{"a host that only looks like localhost", "http://localhost.example.test", false},
		{"credentials in the address", "https://user:hunter2@ballast.example.test", false},
		{"a query", "https://ballast.example.test/?x=1", false},
		{"a fragment", "https://ballast.example.test/#top", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewClient(c.address, aToken)

			if c.accepted && err != nil {
				t.Errorf("%q was refused: %v", c.address, err)
			}
			if !c.accepted {
				if err == nil {
					t.Fatalf("%q was accepted", c.address)
				}
				// It says which variable to change.
				if !strings.Contains(err.Error(), "BALLAST_URL") {
					t.Errorf("the refusal does not name BALLAST_URL: %v", err)
				}
				// And it never repeats what was set: an address can carry a password.
				if strings.Contains(err.Error(), "hunter2") {
					t.Errorf("the refusal repeats the credentials of the address: %v", err)
				}
			}
		})
	}
}

func TestTheClientRefusesATokenItCannotUseWithoutEchoingIt(t *testing.T) {
	for name, token := range map[string]string{
		"none":                          "",
		"only spaces":                   "   ",
		"something that is not a token": "hunter2-this-is-not-a-ballast-token",
		"a token with a line break":     aToken + "\nX-Injected: 1",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewClient("https://ballast.example.test", token)

			if err == nil {
				t.Fatal("the token was accepted")
			}
			if !strings.Contains(err.Error(), "BALLAST_TOKEN") {
				t.Errorf("the refusal does not name BALLAST_TOKEN: %v", err)
			}
			for _, leaked := range []string{"hunter2", "X-Injected", strings.TrimSpace(token)} {
				if leaked != "" && strings.Contains(err.Error(), leaked) {
					t.Errorf("the refusal repeats what was set (%q): %v", leaked, err)
				}
			}
		})
	}
}

// --- How the token is sent ----------------------------------------------------------------

func TestTheTokenTravelsInTheAuthorizationHeaderAndNowhereElse(t *testing.T) {
	var seen []*http.Request
	client, _ := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Clone(context.Background()))
		switch r.Method {
		case http.MethodPost:
			writeJSON(w, http.StatusAccepted, `{"id":"r1","status":"queued"}`)
		case http.MethodDelete:
			writeJSON(w, http.StatusOK, `{"id":"r1","status":"cancelled","outcome":"cancelled"}`)
		default:
			writeJSON(w, http.StatusOK, `{"id":"r1","status":"queued","verdict":"none","thresholds":[]}`)
		}
	})
	ctx := context.Background()

	_, _ = client.Launch(ctx, LaunchRequest{ScenarioID: "s", EnvironmentID: "e", Profile: "smoke"})
	_, _ = client.Run(ctx, "r1")
	_, _ = client.Cancel(ctx, "r1")

	if len(seen) != 3 {
		t.Fatalf("%d requests were sent, want 3", len(seen))
	}
	for _, r := range seen {
		if got := r.Header.Get("Authorization"); got != "Bearer "+aToken {
			t.Errorf("%s %s: Authorization is %q", r.Method, r.URL.Path, got)
		}
		if strings.Contains(r.URL.String(), aToken) || strings.Contains(r.URL.RawQuery, "blt_") {
			t.Errorf("%s: the token is in the address: %s", r.Method, r.URL)
		}
		for name, values := range r.Header {
			if name == "Authorization" {
				continue
			}
			for _, v := range values {
				if strings.Contains(v, aToken) {
					t.Errorf("%s: the token is in the header %s", r.Method, name)
				}
			}
		}
		if r.Header.Get("User-Agent") == "" || strings.HasPrefix(r.Header.Get("User-Agent"), "Go-http-client") {
			t.Errorf("the request does not say what it comes from: %q", r.Header.Get("User-Agent"))
		}
	}
}

func TestARequestGoesUnderThePrefixOfTheAddress(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		writeJSON(w, http.StatusOK, `{"id":"r1","status":"queued","verdict":"none","thresholds":[]}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/ballast/", aToken)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Run(context.Background(), "r1"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if path != "/ballast/runs/r1" {
		t.Errorf("the request went to %q, want /ballast/runs/r1", path)
	}
}

// --- Launching -----------------------------------------------------------------------------

func TestLaunchSendsTheRunAsTheAPIExpectsItAndReturnsWhatWasQueued(t *testing.T) {
	var body map[string]any
	var method, path string
	client, _ := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusAccepted, `{"id":"8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11","status":"queued"}`)
	})

	queued, err := client.Launch(context.Background(), LaunchRequest{
		ScenarioID: "scenario-1", EnvironmentID: "environment-1", Profile: "smoke",
		CI: &CIMetadata{Provider: "github_actions", Repository: "myballast/ballast", Commit: "3f9c2a1b"},
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	if method != http.MethodPost || path != "/runs" {
		t.Errorf("the request was %s %s, want POST /runs", method, path)
	}
	if queued.ID != "8f14e45f-ceea-467a-9e2a-3f1f0f5b1f11" || queued.Status != "queued" {
		t.Errorf("queued %+v", queued)
	}
	if body["scenario_id"] != "scenario-1" || body["environment_id"] != "environment-1" || body["profile"] != "smoke" {
		t.Errorf("body %v", body)
	}
	ci, _ := body["ci"].(map[string]any)
	if ci["provider"] != "github_actions" || ci["repository"] != "myballast/ballast" || ci["commit"] != "3f9c2a1b" {
		t.Errorf("ci %v", body["ci"])
	}
	// Nothing was said about the parts of it that are empty.
	if _, present := ci["branch"]; present {
		t.Errorf("an empty branch was sent: %v", ci)
	}
}

func TestARunWithoutCIMetadataSendsNoCIObject(t *testing.T) {
	var body map[string]any
	client, _ := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusAccepted, `{"id":"r1","status":"queued"}`)
	})

	if _, err := client.Launch(context.Background(), LaunchRequest{ScenarioID: "s", EnvironmentID: "e", Profile: "smoke"}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if _, present := body["ci"]; present {
		t.Errorf("a ci object was sent with nothing in it: %v", body)
	}
}

// A refusal is the server speaking, and what it says is what is printed: an
// unverified target, a window that is closed, a limit that was reached.
func TestARefusalCarriesTheServersMessageAndStatus(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		retryAfter string
		message    string
		wait       time.Duration
	}{
		{"an unverified target", 403, `{"error":"this environment has not been verified"}`, "", "this environment has not been verified", 0},
		{"a refused profile", 422, `{"error":"unknown profile \"burst\""}`, "", `unknown profile "burst"`, 0},
		{"a limit", 429, `{"error":"you have started 30 runs in the last hour, the most allowed"}`, "120",
			"you have started 30 runs in the last hour, the most allowed", 120 * time.Second},
		{"a limit with no wait to give", 429, `{"error":"20 runs are already queued"}`, "", "20 runs are already queued", 0},
		{"the switch", 503, `{"error":"Ballast is not accepting new runs"}`, "", "Ballast is not accepting new runs", 0},
		{"authentication", 401, `{"error":"unauthenticated"}`, "", "unauthenticated", 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, _ := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
				if c.retryAfter != "" {
					w.Header().Set("Retry-After", c.retryAfter)
				}
				writeJSON(w, c.status, c.body)
			})

			_, err := client.Launch(context.Background(), LaunchRequest{ScenarioID: "s", EnvironmentID: "e", Profile: "smoke"})

			var refused *APIError
			if !errors.As(err, &refused) {
				t.Fatalf("got %v, want an API error", err)
			}
			if refused.Status != c.status || refused.Message != c.message || refused.RetryAfter != c.wait {
				t.Errorf("status %d, message %q, wait %s; want %d, %q, %s",
					refused.Status, refused.Message, refused.RetryAfter, c.status, c.message, c.wait)
			}
			if !strings.Contains(err.Error(), c.message) {
				t.Errorf("the error does not say what the server said: %v", err)
			}
			// A refusal is definite: nothing was queued, so nothing is hinted at.
			if errors.Is(err, ErrMayHaveQueued) {
				t.Errorf("a refusal is not an uncertain launch: %v", err)
			}
		})
	}
}

// A request that starts a run is never repeated: there is no key that would let
// the server tell a retry from a second run, and two runs are two loads on one
// target.
func TestLaunchIsNeverRepeatedAndSaysWhenARunMayHaveBeenQueued(t *testing.T) {
	cases := []struct {
		name      string
		behaviour func(w http.ResponseWriter)
		uncertain bool
	}{
		{"a 500", func(w http.ResponseWriter) { writeJSON(w, 500, `{"error":"something went wrong"}`) }, true},
		{"a 502", func(w http.ResponseWriter) { writeJSON(w, 502, `<html>bad gateway</html>`) }, true},
		{"a 504", func(w http.ResponseWriter) { writeJSON(w, 504, ``) }, true},
		{"a connection dropped before an answer", func(w http.ResponseWriter) {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
		}, true},
		// The server said, in so many words, that it did not read the request.
		{"a 503", func(w http.ResponseWriter) { writeJSON(w, 503, `{"error":"authentication is unavailable, try again"}`) }, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var calls atomic.Int32
			client, _ := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				c.behaviour(w)
			})

			_, err := client.Launch(context.Background(), LaunchRequest{ScenarioID: "s", EnvironmentID: "e", Profile: "smoke"})

			if err == nil {
				t.Fatal("no error")
			}
			if n := calls.Load(); n != 1 {
				t.Errorf("the request was sent %d times, want exactly once", n)
			}
			if got := errors.Is(err, ErrMayHaveQueued); got != c.uncertain {
				t.Errorf("may have queued: %v, want %v (%v)", got, c.uncertain, err)
			}
			if c.uncertain && !strings.Contains(err.Error(), "check the project's runs") {
				t.Errorf("the error does not tell what to do about it: %v", err)
			}
		})
	}
}

// --- Reading and cancelling ----------------------------------------------------------------

func TestARunIsReadWithItsStatusVerdictAndThresholds(t *testing.T) {
	client, _ := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{
			"id": "r1", "status": "failed", "verdict": "breached",
			"url": "https://app.ballast.test/runs/r1", "error_message": "",
			"thresholds": [
				{"metric": "http_req_duration", "expression": "p(95)<500", "passed": true},
				{"metric": "http_req_failed", "expression": "rate<0.01", "passed": false}
			],
			"a_field_added_later": {"x": 1}
		}`)
	})

	state, err := client.Run(context.Background(), "r1")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if state.Status != "failed" || state.Verdict != "breached" || state.URL != "https://app.ballast.test/runs/r1" {
		t.Errorf("state %+v", state)
	}
	if len(state.Thresholds) != 2 || state.Thresholds[1].Metric != "http_req_failed" || state.Thresholds[1].Passed {
		t.Errorf("thresholds %+v", state.Thresholds)
	}
	if !state.Terminal() {
		t.Error("a failed run is over")
	}
}

func TestOnlyAnOverRunIsTerminal(t *testing.T) {
	for status, terminal := range map[string]bool{
		"queued": false, "running": false, "orphaned": false,
		"passed": true, "failed": true, "errored": true, "timeout": true,
		"cancelled": true, "rejected": true, "skipped": true,
		// A status this version does not know is not an ending: the tool keeps
		// waiting rather than reporting a result nobody described.
		"a_status_from_the_future": false,
	} {
		if got := (RunState{Status: status}).Terminal(); got != terminal {
			t.Errorf("%q: terminal is %v, want %v", status, got, terminal)
		}
	}
}

func TestACancellationIsARequestAndSaysWhichKind(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		outcome string
	}{
		{"a queued run", 200, `{"id":"r1","status":"cancelled","outcome":"cancelled"}`, "cancelled"},
		{"a run under way", 202, `{"id":"r1","status":"running","outcome":"cancellation_requested"}`, "cancellation_requested"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var method, path string
			client, _ := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				method, path = r.Method, r.URL.Path
				writeJSON(w, c.status, c.body)
			})

			result, err := client.Cancel(context.Background(), "r1")
			if err != nil {
				t.Fatalf("Cancel: %v", err)
			}
			if method != http.MethodDelete || path != "/runs/r1" {
				t.Errorf("the request was %s %s", method, path)
			}
			if result.Outcome != c.outcome {
				t.Errorf("outcome %q, want %q", result.Outcome, c.outcome)
			}
		})
	}
}

func TestNotFoundAndAlreadyFinishedAreAnswersAndNotFailures(t *testing.T) {
	for status, message := range map[int]string{404: "no such run", 409: "this run has already finished"} {
		client, _ := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, status, fmt.Sprintf(`{"error":%q}`, message))
		})

		_, err := client.Cancel(context.Background(), "r1")

		var answered *APIError
		if !errors.As(err, &answered) || answered.Status != status || answered.Message != message {
			t.Errorf("status %d: got %v", status, err)
		}
	}
}

// --- Where the token must not go -------------------------------------------------------------

// A redirect is the server telling the client to send the same request somewhere
// else. With a token in it, that is the token going somewhere nobody chose.
func TestARedirectIsNotFollowed(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elsewhere.Add(1)
		writeJSON(w, http.StatusOK, `{"id":"r1","status":"queued","thresholds":[]}`)
	}))
	defer other.Close()

	for _, status := range []int{301, 302, 307, 308} {
		client, _ := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, other.URL+"/runs/r1", status)
		})

		_, err := client.Run(context.Background(), "r1")

		if err == nil {
			t.Fatalf("%d: a redirect was taken as an answer", status)
		}
		if !strings.Contains(err.Error(), "redirect") {
			t.Errorf("%d: the error does not say what happened: %v", status, err)
		}
	}
	if n := elsewhere.Load(); n != 0 {
		t.Errorf("%d request(s) reached the place the server redirected to: the token went with them", n)
	}
}

// --- What is printed ---------------------------------------------------------------------------

// A server may say anything in an error: a proxy in front of it, a framework that
// prints the request, a hostile endpoint. None of it may carry the token to a log.
func TestTheTokenIsNeverInAnythingTheClientReturnsOrPrints(t *testing.T) {
	bodies := map[string]string{
		"the token as a message":      `{"error":"bad credential ` + aToken + `"}`,
		"the header echoed":           `{"error":"got Authorization: Bearer ` + aToken + `"}`,
		"a bearer without its header": `{"error":"token Bearer ` + aToken + ` is not valid"}`,
		"plain text":                  `request failed for ` + aToken,
		"a debug dump":                `{"error":"x","debug":{"headers":{"Authorization":"Bearer ` + aToken + `"}}}`,
	}

	for name, body := range bodies {
		for _, status := range []int{400, 401, 403, 404, 409, 422, 429, 500, 503} {
			t.Run(fmt.Sprintf("%s/%d", name, status), func(t *testing.T) {
				client, _ := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
					writeJSON(w, status, body)
				})
				ctx := context.Background()

				_, e1 := client.Launch(ctx, LaunchRequest{ScenarioID: "s", EnvironmentID: "e", Profile: "smoke"})
				_, e2 := client.Run(ctx, "r1")
				_, e3 := client.Cancel(ctx, "r1")

				for _, err := range []error{e1, e2, e3} {
					if err == nil {
						t.Fatal("no error")
					}
					for _, shown := range []string{err.Error(), fmt.Sprintf("%v|%+v|%#v|%q", err, err, err, err)} {
						if strings.Contains(shown, aToken) || strings.Contains(shown, aToken[4:]) {
							t.Errorf("the token is in an error: %s", shown)
						}
					}
				}
			})
		}
	}
}

func TestTheClientDoesNotPrintItsOwnToken(t *testing.T) {
	client, err := NewClient("https://ballast.example.test", aToken)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	for _, shown := range []string{
		fmt.Sprint(client), fmt.Sprintf("%v", client), fmt.Sprintf("%+v", client),
		fmt.Sprintf("%#v", client), fmt.Sprintf("%s", client), fmt.Sprintf("%v", *client),
	} {
		if strings.Contains(shown, aToken) || strings.Contains(shown, aToken[4:]) {
			t.Errorf("the token is in what a debug print shows: %s", shown)
		}
	}
}

// What the client was told to redact is redacted wherever it is asked to be.
func TestTheClientRedactsWhatItIsHanded(t *testing.T) {
	client, err := NewClient("https://ballast.example.test", aToken)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got := client.Redact("line one " + aToken + " and Authorization: Bearer " + aToken + " end")

	if strings.Contains(got, aToken) || strings.Contains(got, aToken[4:]) {
		t.Errorf("the token survived: %q", got)
	}
}

func TestWhatTheServerSaysIsMadeSafeToPrint(t *testing.T) {
	cases := []struct {
		name, body string
		check      func(t *testing.T, message string)
	}{
		{"terminal escape sequences", `{"error":"\u001b[2J\u001b[31mpwned\u001b[0m"}`, func(t *testing.T, m string) {
			if strings.ContainsRune(m, 0x1b) {
				t.Errorf("an escape sequence survived: %q", m)
			}
		}},
		{"line breaks", `{"error":"first\nsecond\r\nthird"}`, func(t *testing.T, m string) {
			if strings.ContainsAny(m, "\r\n") {
				t.Errorf("a line break survived, and a log line could be forged with it: %q", m)
			}
		}},
		{"a very long message", `{"error":"` + strings.Repeat("x", 100000) + `"}`, func(t *testing.T, m string) {
			if len([]rune(m)) > 1000 {
				t.Errorf("%d characters were kept", len([]rune(m)))
			}
		}},
		{"a page that is not JSON", `<html><body>502 Bad Gateway</body></html>`, func(t *testing.T, m string) {
			if strings.Contains(m, "<html>") {
				t.Errorf("a page was printed as if it were a message: %q", m)
			}
			if m == "" {
				t.Error("nothing at all was said")
			}
		}},
		{"JSON with no message", `{"detail":"x"}`, func(t *testing.T, m string) {
			if m == "" {
				t.Error("nothing at all was said")
			}
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client, _ := clientFor(t, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 422, c.body) })

			_, err := client.Run(context.Background(), "r1")

			var answered *APIError
			if !errors.As(err, &answered) {
				t.Fatalf("got %v", err)
			}
			c.check(t, answered.Message)
			c.check(t, err.Error())
		})
	}
}

// --- Time --------------------------------------------------------------------------------------

func TestAServerThatNeverAnswersIsGivenUpOn(t *testing.T) {
	release := make(chan struct{})
	client, _ := clientFor(t, func(_ http.ResponseWriter, _ *http.Request) {
		<-release
	}, WithRequestTimeout(150*time.Millisecond))
	defer close(release)

	start := time.Now()
	_, err := client.Run(context.Background(), "r1")

	if err == nil {
		t.Fatal("no error")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("the call took %s with a timeout of 150ms", took)
	}
}

func TestACancelledContextStopsTheCall(t *testing.T) {
	release := make(chan struct{})
	client, _ := clientFor(t, func(_ http.ResponseWriter, _ *http.Request) { <-release })
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := client.Run(ctx, "r1")

	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want the context's cancellation", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("the call outlived its context by %s", took)
	}
}

// A message is cut to length after the token is taken out of it, and not before:
// cut first, and the cut can land inside the token and leave half of it printed.
func TestACutMessageNeverLeavesHalfAToken(t *testing.T) {
	for _, padding := range []int{480, 490, 495, 497, 499, 500} {
		message := strings.Repeat("x", padding) + " " + aToken + " and more text after it"
		client, _ := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 422, fmt.Sprintf(`{"error":%q}`, message))
		})

		_, err := client.Run(context.Background(), "r1")

		var answered *APIError
		if !errors.As(err, &answered) {
			t.Fatalf("got %v", err)
		}
		// Not the token, and not the start of it either.
		if strings.Contains(answered.Message, "blt_") || strings.Contains(answered.Message, aToken[4:10]) {
			t.Errorf("padding %d: part of the token was printed: ...%s", padding, answered.Message[len(answered.Message)-30:])
		}
	}
}

// An identifier that comes back from the server ends up in the next address, with
// the token riding on the request. One that is not an identifier is refused before
// anything is sent.
func TestAnIdentifierThatIsNotOneIsNeverPutInAnAddress(t *testing.T) {
	var requests atomic.Int32
	client, _ := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeJSON(w, http.StatusOK, `{"id":"r1","status":"queued","thresholds":[]}`)
	})

	for _, id := range []string{"../admin", "r1/../../x", "r1?x=1", "r1#x", "", "r 1", "r1\n", strings.Repeat("a", 65), "%2e%2e"} {
		if _, err := client.Run(context.Background(), id); err == nil {
			t.Errorf("Run(%q) was sent", id)
		}
		if _, err := client.Cancel(context.Background(), id); err == nil {
			t.Errorf("Cancel(%q) was sent", id)
		}
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("%d request(s) left for an identifier that is not one", n)
	}
}

// A launch whose answer carries such an identifier is a launch that may have
// queued a run, and the tool says so instead of following it.
func TestALaunchAnsweredWithAnIdentifierThatIsNotOneIsUncertain(t *testing.T) {
	client, _ := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusAccepted, `{"id":"../../admin","status":"queued"}`)
	})

	_, err := client.Launch(context.Background(), LaunchRequest{ScenarioID: "s", EnvironmentID: "e", Profile: "smoke"})

	if !errors.Is(err, ErrMayHaveQueued) {
		t.Errorf("got %v, want an uncertain launch", err)
	}
}
