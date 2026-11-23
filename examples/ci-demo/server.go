package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxSlowMS is the longest /slow will wait. It is a demonstration of a slow endpoint, and
// a service that anybody can ask to hold a connection for a minute is a small way to
// exhaust it.
const maxSlowMS = 3000

// challengePrefix is where Ballast looks for the proof that a target is yours:
// /.well-known/ballast-<token>, at the root of the origin.
const challengePrefix = "/.well-known/ballast-"

// tokenShape is what a token may look like: the base64url alphabet, and nothing that
// could make a path of its own.
var tokenShape = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// challenge is the proof of ownership: a token, served at one path.
type challenge struct {
	token string
}

// newChallenge returns the proof for a token, or none when there is no token. A token that
// is not a path segment is an error: it would be served at a path nobody asked for.
func newChallenge(token string) (challenge, error) {
	if token == "" {
		return challenge{}, nil
	}
	if !tokenShape.MatchString(token) {
		return challenge{}, errors.New("the challenge token is not made of letters, digits, - and _")
	}
	return challenge{token: token}, nil
}

// serves reports whether path is exactly the one the challenge is served at.
func (c challenge) serves(path string) bool {
	if c.token == "" || !strings.HasPrefix(path, challengePrefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(path[len(challengePrefix):]), []byte(c.token)) == 1
}

// NewHandler is the service. The token is the challenge of the target, or empty.
func NewHandler(token string) http.Handler {
	proof, err := newChallenge(token)
	if err != nil {
		// A token that cannot be served is not served: the proof is simply absent, and
		// the verification of the target fails, which says what is wrong.
		proof = challenge{}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", readOnly(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	}))
	mux.HandleFunc("/items", readOnly(items))
	mux.HandleFunc("/slow", readOnly(slow))
	mux.HandleFunc("/.well-known/", readOnly(func(w http.ResponseWriter, r *http.Request) {
		if !proof.serves(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		// The token is the whole content: Ballast compares what it reads with what it
		// issued, and a newline of ours would be part of the comparison.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(proof.token))
	}))
	return mux
}

// readOnly lets GET and HEAD through and refuses everything else.
func readOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

func items(w http.ResponseWriter, _ *http.Request) {
	list := []map[string]any{
		{"id": 1, "name": "anchor"},
		{"id": 2, "name": "ballast"},
		{"id": 3, "name": "keel"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"count": len(list), "items": list})
}

// slow answers after the time it is asked for, in milliseconds, at most maxSlowMS. What is
// typed is a whole number or the request is refused, and none of it is repeated.
func slow(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()["ms"]
	if len(values) != 1 {
		http.Error(w, "ms must be given once, as a whole number of milliseconds", http.StatusBadRequest)
		return
	}

	raw := values[0]
	ms, err := strconv.Atoi(raw)
	// Atoi accepts a sign and nothing else it should not, but a "+5" is not what was meant.
	if err != nil || raw != strconv.Itoa(ms) || ms < 0 || ms > maxSlowMS {
		http.Error(w, "ms must be a whole number of milliseconds, at most "+strconv.Itoa(maxSlowMS), http.StatusBadRequest)
		return
	}

	select {
	case <-time.After(time.Duration(ms) * time.Millisecond):
	case <-r.Context().Done():
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"waited_ms": ms})
}

// listenAddress is ":PORT", from the environment, and ":8080" for anything that is not a
// port.
func listenAddress(port string) string {
	if n, err := strconv.Atoi(port); err == nil && n > 0 && n < 65536 {
		return ":" + strconv.Itoa(n)
	}
	return ":8080"
}
