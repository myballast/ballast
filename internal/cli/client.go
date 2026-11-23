// Package cli is what the ballast command line tool does.
//
// It runs on somebody else's machine, in their pipeline, with a Ballast API token
// in its environment and its output in a log other people read. Everything here is
// written for that: the token is sent one way and printed never, an address that
// would send it somewhere unsafe is refused, a redirect is not followed, and a
// request that may have started a run is not repeated.
//
// The package imports nothing from internal/adapter: the tool talks to the API the
// way any client does, over HTTP, with types of its own.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/youssefmahersi/ballast/internal/domain"
)

const (
	// defaultRequestTimeout bounds one request that is not a stream. A stream has
	// no such bound: it lasts as long as the run does.
	defaultRequestTimeout = 30 * time.Second

	// maxResponseBytes bounds what is read back from a request that is not a
	// stream. A run with a few hundred metrics is far below it; a server that
	// answers with more is not one to read to the end.
	maxResponseBytes = 8 << 20

	// maxMessageRunes bounds what the server's words are cut to before printing.
	maxMessageRunes = 500
)

// ErrMayHaveQueued marks a launch whose outcome is not known: the request left,
// and no answer that settles it came back. It is the one failure after which a
// run may exist, and the only one a caller must tell somebody about.
var ErrMayHaveQueued = errors.New("the run may have been queued")

// tokenShape is what a Ballast API token looks like: the prefix, then the
// URL-safe alphabet of its encoding. Anything else is refused before it is sent,
// which also keeps a stray line break from reaching a header.
var tokenShape = regexp.MustCompile(`^` + regexp.QuoteMeta(domain.APITokenPrefix) + `[A-Za-z0-9_-]+$`)

// runIDShape is what an identifier read back from the server must look like
// before it is put into an address. A server that answered with "../x" would
// otherwise steer the next request, and the token with it, to another path.
var runIDShape = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// Option adjusts a Client.
type Option func(*Client)

// WithRequestTimeout bounds each request that is not a stream.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithStreamBackoff sets how long to wait before the nth reconnection of a log
// stream. The default grows and is capped; tests ask for none.
func WithStreamBackoff(f func(attempt int) time.Duration) Option {
	return func(c *Client) {
		if f != nil {
			c.streamBackoff = f
		}
	}
}

// WithStreamIdleTimeout sets how long a log stream may stay silent before it is
// taken for dead and reconnected. The server sends a comment every fifteen
// seconds, so silence for longer than that means something.
func WithStreamIdleTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.streamIdle = d
		}
	}
}

// Client talks to the Ballast API with one token.
type Client struct {
	base          *url.URL
	token         string
	http          *http.Client
	timeout       time.Duration
	redactor      domain.Redactor
	streamBackoff func(attempt int) time.Duration
	streamIdle    time.Duration
}

// NewClient returns a client for the API at baseURL, authenticated with token.
//
// Both are refused when they would be unsafe or unusable, and the refusal names
// the variable to change and never repeats what it was set to: an address can
// carry a password, and a token that is almost right is nearly a token.
func NewClient(baseURL, token string, opts ...Option) (*Client, error) {
	base, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("BALLAST_TOKEN is not set: create a token in the project's \"CI tokens\" page and pass it in the environment")
	}
	if !tokenShape.MatchString(token) {
		return nil, fmt.Errorf("BALLAST_TOKEN is not a Ballast API token: it starts with %q and holds nothing but letters, digits, - and _", domain.APITokenPrefix)
	}

	redactor, _ := domain.NewRedactor(map[string]string{"BALLAST_TOKEN": token})

	c := &Client{
		base: base, token: token, timeout: defaultRequestTimeout, redactor: redactor,
		streamBackoff: defaultStreamBackoff, streamIdle: defaultStreamIdleTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = c.timeout
	c.http = &http.Client{
		Transport: transport,
		// A redirect is the server telling the client to send the same request
		// somewhere else, with the token in it. It is reported, never followed.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return c, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("BALLAST_URL is not set: it is the address of your Ballast, for example https://ballast.example.com")
	}

	// A parse error from net/url repeats the text it was given, which may hold a
	// password; the refusal below is written here instead.
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return nil, errors.New("BALLAST_URL is not an address: write it with its scheme, for example https://ballast.example.com")
	}

	switch {
	case parsed.User != nil:
		return nil, errors.New("BALLAST_URL must not carry a user name or a password: the token is the credential")
	case parsed.RawQuery != "" || parsed.Fragment != "":
		return nil, errors.New("BALLAST_URL must be an address without a query or a fragment")
	case parsed.Scheme == "https":
	case parsed.Scheme == "http" && isLoopback(parsed.Hostname()):
	case parsed.Scheme == "http":
		return nil, errors.New("BALLAST_URL must be https: over plain http the token would cross the network in the clear (http is accepted only for localhost)")
	default:
		return nil, errors.New("BALLAST_URL must start with https://")
	}
	return parsed, nil
}

// isLoopback says whether a host never leaves the machine.
func isLoopback(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Format prints the client without its token, whatever verb is used.
//
// A client is the one value that holds the secret, and somebody debugging a
// pipeline prints values: %v, %+v and %#v would otherwise show every field,
// the token and its copy inside the redactor among them.
func (c Client) Format(f fmt.State, _ rune) {
	address := ""
	if c.base != nil {
		address = c.base.String()
	}
	_, _ = fmt.Fprintf(f, "cli.Client{base: %s, token: %s}", address, domain.Redacted)
}

// Redact removes the token, and anything that looks like a credential, from text
// that is about to be printed.
func (c *Client) Redact(text string) string {
	return c.redactor.Redact(text)
}

// --- Errors -------------------------------------------------------------------------------------

// APIError is the server saying no, or saying something this tool did not expect.
type APIError struct {
	// Status is the HTTP status.
	Status int
	// Message is the server's own sentence, made safe to print. For an answer
	// that carries none, it says so instead of printing the body: a page from a
	// proxy is not a message.
	Message string
	// RetryAfter is how long the server asked to wait, zero when it did not say.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
}

// uncertainLaunch is a launch whose outcome is not known.
type uncertainLaunch struct{ cause error }

func (e *uncertainLaunch) Error() string {
	return e.cause.Error() + ". The request may have reached Ballast and queued a run: check the project's runs before launching again"
}
func (e *uncertainLaunch) Unwrap() error        { return e.cause }
func (e *uncertainLaunch) Is(target error) bool { return target == ErrMayHaveQueued }

// --- Requests ---------------------------------------------------------------------------------------

// LaunchRequest is what starts a run.
type LaunchRequest struct {
	ScenarioID    string      `json:"scenario_id"`
	EnvironmentID string      `json:"environment_id"`
	Profile       string      `json:"profile"`
	CI            *CIMetadata `json:"ci,omitempty"`
}

// CIMetadata is what the pipeline says about itself. Declared, never proved.
type CIMetadata struct {
	Provider   string `json:"provider"`
	Repository string `json:"repository,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Commit     string `json:"commit,omitempty"`
	JobURL     string `json:"job_url,omitempty"`
}

// Queued is what a launch returns.
type Queued struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Threshold is k6's verdict on one rule.
type Threshold struct {
	Metric     string `json:"metric"`
	Expression string `json:"expression"`
	Passed     bool   `json:"passed"`
}

// RunState is a run as the tool reads it while it waits.
type RunState struct {
	ID           string      `json:"id"`
	Status       string      `json:"status"`
	Verdict      string      `json:"verdict"`
	URL          string      `json:"url"`
	ErrorMessage string      `json:"error_message"`
	Thresholds   []Threshold `json:"thresholds"`
}

// Terminal reports whether the run is over. A status this version does not know
// is not an ending: the tool keeps waiting rather than report a result nobody
// described to it.
func (s RunState) Terminal() bool {
	status := domain.RunStatus(s.Status)
	return status.Known() && status.IsTerminal()
}

// CancelResult says what a cancellation did.
type CancelResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Outcome is "cancelled" for a run that had not started, and
	// "cancellation_requested" for one under way, which is only asked to stop.
	Outcome string `json:"outcome"`
}

// Launch starts a run.
//
// It is sent exactly once. Nothing identifies a retry to the server, so a second
// attempt after a lost answer could be a second run, two loads on one target. When
// the request left and no answer settled it, the error is ErrMayHaveQueued.
func (c *Client) Launch(ctx context.Context, request LaunchRequest) (Queued, error) {
	status, header, body, err := c.do(ctx, http.MethodPost, []string{"runs"}, request)
	if err != nil {
		return Queued{}, &uncertainLaunch{cause: err}
	}

	if status < 200 || status > 299 {
		refusal := c.refusal(status, header, body)
		// The server said, in so many words, that it did not read the request, or
		// that it refused it: nothing was queued. Any other failure of its own
		// might have come after the run was written.
		if status >= 500 && status != http.StatusServiceUnavailable {
			return Queued{}, &uncertainLaunch{cause: refusal}
		}
		return Queued{}, refusal
	}

	var queued Queued
	if err := json.Unmarshal(body, &queued); err != nil || !runIDShape.MatchString(queued.ID) {
		return Queued{}, &uncertainLaunch{cause: errors.New("the request was accepted but the answer could not be read")}
	}
	queued.Status = c.clean(queued.Status)
	return queued, nil
}

// Run reads a run.
func (c *Client) Run(ctx context.Context, id string) (RunState, error) {
	status, header, body, err := c.do(ctx, http.MethodGet, []string{"runs", id}, nil)
	if err != nil {
		return RunState{}, err
	}
	if status < 200 || status > 299 {
		return RunState{}, c.refusal(status, header, body)
	}

	var state RunState
	if err := json.Unmarshal(body, &state); err != nil {
		return RunState{}, errors.New("the answer about the run could not be read")
	}

	// What comes back is printed, by a tool whose output is read in a log.
	state.Status = c.clean(state.Status)
	state.Verdict = c.clean(state.Verdict)
	state.URL = c.clean(state.URL)
	state.ErrorMessage = c.clean(state.ErrorMessage)
	for i := range state.Thresholds {
		state.Thresholds[i].Metric = c.clean(state.Thresholds[i].Metric)
		state.Thresholds[i].Expression = c.clean(state.Thresholds[i].Expression)
	}
	return state, nil
}

// Cancel asks for a run to be cancelled. It is a request: a run under way is
// stopped by the worker running it, a second or two later.
func (c *Client) Cancel(ctx context.Context, id string) (CancelResult, error) {
	status, header, body, err := c.do(ctx, http.MethodDelete, []string{"runs", id}, nil)
	if err != nil {
		return CancelResult{}, err
	}
	if status < 200 || status > 299 {
		return CancelResult{}, c.refusal(status, header, body)
	}

	var result CancelResult
	if err := json.Unmarshal(body, &result); err != nil {
		return CancelResult{}, errors.New("the answer about the cancellation could not be read")
	}
	result.Status = c.clean(result.Status)
	result.Outcome = c.clean(result.Outcome)
	return result, nil
}

// do sends one request and reads the whole answer.
func (c *Client) do(
	ctx context.Context, method string, segments []string, payload any,
) (int, http.Header, []byte, error) {
	for _, segment := range segments {
		if !runIDShape.MatchString(segment) {
			return 0, nil, nil, errors.New("an identifier from Ballast is not one: refusing to put it in an address")
		}
	}

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("the request could not be written: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(callCtx, method, c.base.JoinPath(segments...).String(), body)
	if err != nil {
		return 0, nil, nil, errors.New("the request could not be built")
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", userAgent())
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return 0, nil, nil, c.transportError(ctx, err)
	}
	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return 0, nil, nil, c.transportError(ctx, err)
	}
	if len(raw) > maxResponseBytes {
		return 0, nil, nil, errors.New("the answer is larger than this tool will read")
	}
	return response.StatusCode, response.Header, raw, nil
}

// transportError describes a request that got no answer. The caller's own
// cancellation is returned as it is, so that it can be recognised.
func (c *Client) transportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	inner := err
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		inner = urlErr.Err
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return fmt.Errorf("no answer from %s within %s", c.base.Host, c.timeout)
	}
	return fmt.Errorf("could not reach %s: %s", c.base.Host, c.clean(inner.Error()))
}

func isTimeout(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

// refusal turns an answer that is not a success into an error.
func (c *Client) refusal(status int, header http.Header, body []byte) *APIError {
	refusal := &APIError{Status: status}

	if status >= 300 && status <= 399 {
		refusal.Message = "the server redirected the request, and Ballast does not follow redirects, because the token would follow it: check BALLAST_URL"
		return refusal
	}

	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && strings.TrimSpace(parsed.Error) != "" {
		refusal.Message = c.clean(parsed.Error)
	} else {
		refusal.Message = "no message came with the answer"
	}

	if seconds, err := strconv.Atoi(strings.TrimSpace(header.Get("Retry-After"))); err == nil && seconds > 0 {
		refusal.RetryAfter = time.Duration(seconds) * time.Second
	}
	return refusal
}

// clean makes a piece of the server's text safe to print into a log.
//
// Control characters go — an escape sequence can rewrite a terminal, and a line
// break can forge a line of the log — and so do the invisible characters that make
// text read differently from what it is. What is left is redacted, and only then
// cut to length, so that the end of a long message cannot split a token in two and
// leave half of it behind.
func (c *Client) clean(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || isInvisible(r) {
			return ' '
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	text = c.redactor.Redact(text)

	if runes := []rune(text); len(runes) > maxMessageRunes {
		text = string(runes[:maxMessageRunes]) + "…"
	}
	return text
}

// isInvisible reports the characters that are not control characters and still
// change how a line reads: zero-width and directional marks, line separators, the
// byte order mark.
func isInvisible(r rune) bool {
	switch {
	case r >= 0x200b && r <= 0x200f,
		r >= 0x2028 && r <= 0x202e,
		r >= 0x2060 && r <= 0x2064,
		r >= 0x2066 && r <= 0x2069,
		r == 0xfeff:
		return true
	}
	return false
}
