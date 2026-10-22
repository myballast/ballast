package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/adapter/curl"
	"github.com/youssefmahersi/ballast/internal/adapter/har"
	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/domain"
)

const (
	recordedSession = "sess-0123456789abcdefghijkl"
	recordedToken   = "ghp_a_token_in_a_recording_0123456789abcdef"
	recordedAPIKey  = "ak_live_0123456789abcdefghijklmnopqrstuv"
)

// A recording that holds, in every place a secret can sit, a cookie and a token.
var recordedHAR = `{"log": {"entries": [
  {"_resourceType": "xhr",
   "request": {"method": "POST", "url": "https://shop.example.com/api/login?access_token=` + recordedToken + `",
     "headers": [
       {"name": "Cookie", "value": "sid=` + recordedSession + `"},
       {"name": "Authorization", "value": "Bearer ` + recordedToken + `"},
       {"name": "X-Api-Key", "value": "` + recordedAPIKey + `"},
       {"name": "Content-Type", "value": "application/json"}],
     "postData": {"mimeType": "application/json", "text": "{\"user\":\"a\",\"password\":\"` + recordedToken + `\"}"}},
   "response": {"content": {"mimeType": "application/json", "text": "{\"setup\":\"` + recordedSession + `\"}"}}},
  {"_resourceType": "fetch",
   "request": {"method": "GET", "url": "https://shop.example.com/api/orders/42?expand=items",
     "headers": [{"name": "Cookie", "value": "sid=` + recordedSession + `"}, {"name": "Authorization", "value": "Bearer ` + recordedToken + `"}]},
   "response": {"content": {"mimeType": "application/json"}}}
]}}`

var recordedCurl = `curl 'https://shop.example.com/api/login?access_token=` + recordedToken + `' \
  -H 'authorization: Bearer ` + recordedToken + `' \
  -H 'cookie: sid=` + recordedSession + `' \
  -H 'content-type: application/json' \
  --data-raw '{"user":"a","password":"` + recordedToken + `"}'
curl -u bob:hunter2hunter2 https://shop.example.com/api/me`

// The whole path for the formats of a browser and a terminal: read, analysed,
// written as a graph, generated, and checked by the inspector — with no cookie
// and no token in any of it.
func TestARecordingBecomesAScriptWithNeitherCookieNorToken(t *testing.T) {
	formats := map[string]struct {
		read   func() (domain.ImportedCollection, error)
		chosen []int
	}{
		"har":  {func() (domain.ImportedCollection, error) { return har.NewReader().Read([]byte(recordedHAR), nil) }, []int{0, 1}},
		"curl": {func() (domain.ImportedCollection, error) { return curl.NewReader().Read([]byte(recordedCurl), nil) }, []int{0, 1}},
	}

	for name, format := range formats {
		t.Run(name, func(t *testing.T) {
			imported, err := format.read()
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			analysis, err := domain.AnalyseCollection(imported, format.chosen)
			if err != nil {
				t.Fatalf("AnalyseCollection: %v", err)
			}
			if err := analysis.Check(); err != nil {
				t.Fatalf("Check: %v", err)
			}
			graph, err := analysis.Graph()
			if err != nil {
				t.Fatalf("Graph: %v", err)
			}
			script, err := k6.GenerateScript(graph)
			if err != nil {
				t.Fatalf("GenerateScript: %v", err)
			}
			if report := k6.NewScriptInspector().Inspect(script); report.Err() != nil {
				t.Fatalf("the inspector refuses the script: %v\n%s", report.Errors(), script)
			}

			everything := script + dumped(analysis)
			for _, secret := range []string{recordedSession, recordedToken, recordedAPIKey, "hunter2hunter2", "Ym9iOnN"} {
				if strings.Contains(everything, secret) {
					t.Errorf("%q is in the script, the report or the secrets:\n%s", secret, everything)
				}
			}
			if !strings.Contains(script, "__ENV") || !strings.Contains(script, `tags: { name: "POST /api/login" }`) {
				t.Errorf("the script is not what was recorded:\n%s", script)
			}
		})
	}
}

// dumped renders what an import tells the person: the findings, and the keys of
// the secrets to create.
func dumped(analysis domain.CollectionAnalysis) string {
	var out strings.Builder
	for _, finding := range analysis.Findings {
		out.WriteString(finding.Request + " " + finding.Message + "\n")
	}
	for _, secret := range analysis.Secrets {
		out.WriteString(secret.Key + " " + secret.Variable + "\n")
	}
	return out.String()
}

// The guard does not trust the rules before it. A cookie the reader removed that
// turns up anywhere in what is about to be stored, here in a path, stops the
// import and says where, not what.
func TestACookieValueFoundAnywhereRefusesTheImport(t *testing.T) {
	text := `{"log": {"entries": [
	  {"_resourceType": "xhr",
	   "request": {"method": "GET", "url": "https://shop.example.com/api/session/` + recordedSession + `/orders",
	     "headers": [{"name": "Cookie", "value": "sid=` + recordedSession + `"}]},
	   "response": {"content": {"mimeType": "application/json"}}}]}}`

	imported, err := har.NewReader().Read([]byte(text), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	analysis, err := domain.AnalyseCollection(imported, []int{0})
	if err != nil {
		t.Fatalf("AnalyseCollection: %v", err)
	}

	err = analysis.Check()
	if !errors.Is(err, domain.ErrInvalidCollection) {
		t.Fatalf("Check = %v, wanted it to refuse", err)
	}
	if strings.Contains(err.Error(), recordedSession) {
		t.Errorf("the refusal repeats the cookie: %v", err)
	}
}
