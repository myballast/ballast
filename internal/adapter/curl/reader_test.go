package curl

import (
	"errors"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/domain"
)

func read(t *testing.T, text string) domain.ImportedCollection {
	t.Helper()

	imported, err := NewReader().Read([]byte(text), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return imported
}

func header(request domain.CollectionRequest, name string) (string, bool) {
	for _, h := range request.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value, true
		}
	}
	return "", false
}

func notes(imported domain.ImportedCollection) string {
	var all []string
	for _, request := range imported.Requests {
		all = append(all, request.Notes...)
	}
	for _, finding := range imported.Findings {
		all = append(all, finding.Message)
	}
	return strings.Join(all, "\n")
}

func TestAPlainGetIsAGet(t *testing.T) {
	imported := read(t, `curl https://api.example.com/users/3?expand=1`)

	request := imported.Requests[0]
	if request.Method != "GET" || request.Name != "GET /users/3" || request.Host != "api.example.com" ||
		request.URL != "https://api.example.com/users/3?expand=1" {
		t.Errorf("request = %+v", request)
	}
}

func TestTheMethodFollowsCurlsRules(t *testing.T) {
	cases := map[string]string{
		`curl https://x.test/a`:                      "GET",
		`curl -X DELETE https://x.test/a`:            "DELETE",
		`curl -XPUT https://x.test/a`:                "PUT",
		`curl -d a=1 https://x.test/a`:               "POST",
		`curl -G -d a=1 https://x.test/a`:            "GET",
		`curl -I https://x.test/a`:                   "HEAD",
		`curl -X PATCH -d a=1 https://x.test/a`:      "PATCH",
		`curl --request post --url https://x.test/a`: "POST",
		`curl --json '{"a":1}' https://x.test/a`:     "POST",
		`curl -sSL --compressed -k https://x.test/a`: "GET",
	}
	for command, want := range cases {
		if got := read(t, command).Requests[0].Method; got != want {
			t.Errorf("%s: method = %s, wanted %s", command, got, want)
		}
	}
}

func TestGetWithDataPutsTheDataInTheQuery(t *testing.T) {
	request := read(t, `curl -G -d a=1 -d b=2 'https://x.test/s?c=3'`).Requests[0]

	if request.URL != "https://x.test/s?c=3&a=1&b=2" || request.Body.Mode != "" {
		t.Errorf("request = %+v", request)
	}
}

func TestChromesCopyAsCurlIsRead(t *testing.T) {
	text := `curl 'https://shop.example.com/api/cart' \
  -H 'accept: application/json' \
  -H 'accept-language: en-US,en;q=0.9' \
  -H 'content-type: application/json' \
  -H 'cookie: sid=sess-0123456789abcdef; theme=dark' \
  -H 'origin: https://shop.example.com' \
  -H 'sec-fetch-mode: cors' \
  -H 'user-agent: Mozilla/5.0' \
  --data-raw $'{"item":"it\'s","qty":2}'`

	imported := read(t, text)
	request := imported.Requests[0]

	if request.Method != "POST" || request.Body.Language != "json" || request.Body.Raw != `{"item":"it's","qty":2}` {
		t.Errorf("request = %+v", request)
	}
	for _, gone := range []string{"cookie", "origin", "sec-fetch-mode", "user-agent", "accept-language"} {
		if _, found := header(request, gone); found {
			t.Errorf("the header %s was kept", gone)
		}
	}
	if _, found := header(request, "accept"); !found {
		t.Error("the Accept header was dropped")
	}
	if !contains(imported.Removed, "sess-0123456789abcdef") {
		t.Errorf("removed = %v", imported.Removed)
	}
	if said := notes(imported); !strings.Contains(said, "cookie was removed") || strings.Contains(said, "sess-0123") {
		t.Errorf("notes = %q", said)
	}
}

func TestWordsAreCutLikeAShellCutsThem(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{`a b  c`, []string{"a", "b", "c"}},
		{`a 'b c' "d e"`, []string{"a", "b c", "d e"}},
		{`a\ b`, []string{"a b"}},
		{`"a \"q\" \\ \$x"`, []string{`a "q" \ $x`}},
		{"a \\\n b", []string{"a", "b"}},
		{`$'a\nb\t\x41é\'c'`, []string{"a\nb\tAé'c"}},
		{`'' x`, []string{"", "x"}},
		{`a #comment`, []string{"a"}},
		{`a#notacomment`, []string{"a#notacomment"}},
	}
	for _, c := range cases {
		commands := splitCommands(c.text)
		var got []string
		for _, w := range commands[0].words {
			got = append(got, w.text)
		}
		if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
			t.Errorf("%q = %q, wanted %q", c.text, got, c.want)
		}
	}
}

func TestSeveralCommandsAreSeparatedByLinesSemicolonsAndAnds(t *testing.T) {
	imported := read(t, "curl https://x.test/a\ncurl https://x.test/b; curl https://x.test/c && curl https://x.test/d")

	if len(imported.Requests) != 4 {
		t.Fatalf("requests = %d", len(imported.Requests))
	}
	for index, want := range []string{"/a", "/b", "/c", "/d"} {
		if !strings.HasSuffix(imported.Requests[index].Name, want) || imported.Requests[index].Position != index {
			t.Errorf("request %d = %+v", index, imported.Requests[index])
		}
	}
}

func TestWhatFollowsAPipeOrARedirectionIsIgnoredAndSaidSo(t *testing.T) {
	imported := read(t, `curl -s https://x.test/a | jq . > out.json 2>&1`)

	if len(imported.Requests) != 1 || imported.Requests[0].URL != "https://x.test/a" {
		t.Fatalf("requests = %+v", imported.Requests)
	}
	if !strings.Contains(notes(imported), "pipe or a redirection") {
		t.Errorf("notes = %q", notes(imported))
	}
}

func TestNothingIsExpandedOrRun(t *testing.T) {
	imported := read(t, "curl -H \"Authorization: Bearer $TOKEN\" 'https://x.test/a?v=$(whoami)'")

	request := imported.Requests[0]
	if value, _ := header(request, "Authorization"); value != "Bearer $TOKEN" {
		t.Errorf("the variable was replaced: %q", value)
	}
	if !strings.Contains(request.URL, "$(whoami)") {
		t.Errorf("url = %q", request.URL)
	}
	if !strings.Contains(notes(imported), "never expanded") {
		t.Errorf("notes = %q", notes(imported))
	}
}

func TestLinesThatAreNotCurlAreSkippedWithoutBeingReadOrRepeated(t *testing.T) {
	imported := read(t, "export TOKEN=sk_live_abcdefghijklmnop\ncurl https://x.test/a\nrm -rf /")

	if len(imported.Requests) != 1 {
		t.Fatalf("requests = %d", len(imported.Requests))
	}
	said := notes(imported)
	if !strings.Contains(said, "2 lines that are not curl commands") || strings.Contains(said, "sk_live") || strings.Contains(said, "rm") {
		t.Errorf("notes = %q", said)
	}
}

func TestSettingVariablesInFrontOfCurlIsAllowed(t *testing.T) {
	if got := len(read(t, `A=1 B=two sudo curl https://x.test/a`).Requests); got != 1 {
		t.Errorf("requests = %d", got)
	}
}

func TestBasicAuthBecomesASecretVariableAndTheGuardKnowsTheCredentials(t *testing.T) {
	imported := read(t, `curl -u bob:secretpass99 https://x.test/me`)

	request := imported.Requests[0]
	if value, _ := header(request, "Authorization"); value != "Basic {{basic_auth}}" {
		t.Errorf("Authorization = %q", value)
	}
	if len(imported.Variables) != 1 || imported.Variables[0].Name != "basic_auth" || !imported.Variables[0].Secret || imported.Variables[0].Value != "" {
		t.Errorf("variables = %+v", imported.Variables)
	}
	for _, wanted := range []string{"bob:secretpass99", "secretpass99", "Ym9iOnNlY3JldHBhc3M5OQ=="} {
		if !contains(imported.Removed, wanted) {
			t.Errorf("the guard does not know %q: %v", wanted, imported.Removed)
		}
	}
	if said := notes(imported); !strings.Contains(said, "BASIC_AUTH") || strings.Contains(said, "secretpass99") {
		t.Errorf("notes = %q", said)
	}
}

func TestTwoDifferentCredentialsAreTwoSecrets(t *testing.T) {
	imported := read(t, "curl -u a:one111111 https://x.test/a\ncurl -u b:two222222 https://x.test/b\ncurl -u a:one111111 https://x.test/c")

	if len(imported.Variables) != 2 {
		t.Fatalf("variables = %+v", imported.Variables)
	}
	first, _ := header(imported.Requests[0], "Authorization")
	second, _ := header(imported.Requests[1], "Authorization")
	third, _ := header(imported.Requests[2], "Authorization")
	if first == second || first != third {
		t.Errorf("headers = %q %q %q", first, second, third)
	}
}

func TestAHeaderWrittenWithHBeatsDashU(t *testing.T) {
	request := read(t, `curl -u bob:secretpass99 -H 'Authorization: Bearer abc' https://x.test/me`).Requests[0]

	if value, _ := header(request, "Authorization"); value != "Bearer abc" {
		t.Errorf("Authorization = %q", value)
	}
}

func TestABearerOptionBecomesABearerHeader(t *testing.T) {
	request := read(t, `curl --oauth2-bearer tok123 https://x.test/me`).Requests[0]

	if value, _ := header(request, "Authorization"); value != "Bearer tok123" {
		t.Errorf("Authorization = %q", value)
	}
}

func TestDashBOptionRemovesCookiesAndNamesFilesWithoutOpeningThem(t *testing.T) {
	cookies := read(t, `curl -b 'sid=abcdefgh12345; x=1' https://x.test/a`)
	if !contains(cookies.Removed, "abcdefgh12345") || !strings.Contains(notes(cookies), "cookie was removed") {
		t.Errorf("removed = %v, notes = %q", cookies.Removed, notes(cookies))
	}

	file := read(t, `curl -b cookies.txt https://x.test/a`)
	if !strings.Contains(notes(file), "names a file, which is never opened") {
		t.Errorf("notes = %q", notes(file))
	}
}

func TestBodyFromAFileIsNotReadAndSaidSo(t *testing.T) {
	imported := read(t, `curl -d @payload.json https://x.test/a`)

	if imported.Requests[0].Body.Mode != "" || !strings.Contains(notes(imported), "never opened") {
		t.Errorf("body = %+v, notes = %q", imported.Requests[0].Body, notes(imported))
	}
	raw := read(t, `curl --data-raw '@not-a-file' https://x.test/a`)
	if raw.Requests[0].Body.Raw != "@not-a-file" {
		t.Errorf("--data-raw read a file: %+v", raw.Requests[0].Body)
	}
}

func TestFormBodiesAreReadAsFieldsWhenNoContentTypeIsGiven(t *testing.T) {
	body := read(t, `curl -d 'a=1&b=x%20y' -d c=3 https://x.test/a`).Requests[0].Body

	if body.Mode != "urlencoded" || len(body.Pairs) != 3 || body.Pairs[1].Value != "x y" {
		t.Errorf("body = %+v", body)
	}
}

func TestDataUrlencodeEncodesTheContent(t *testing.T) {
	body := read(t, `curl --data-urlencode 'q=a b&c' https://x.test/a`).Requests[0].Body

	if body.Mode != "urlencoded" || body.Pairs[0].Name != "q" || body.Pairs[0].Value != "a b&c" {
		t.Errorf("body = %+v", body)
	}
}

func TestJSONOptionSetsBothHeaders(t *testing.T) {
	request := read(t, `curl --json '{"a":1}' https://x.test/a`).Requests[0]

	if ct, _ := header(request, "Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if request.Body.Language != "json" {
		t.Errorf("body = %+v", request.Body)
	}
}

func TestFormsUploadsProxiesAndUnknownOptionsAreReportedByNameNeverByValue(t *testing.T) {
	imported := read(t, `curl -F file=@secret.pdf -x http://user:proxypass99@proxy:3128 --weird-option value --cert /home/me/key.pem https://x.test/a`)

	said := notes(imported)
	for _, wanted := range []string{"--form", "--proxy", "--weird-option", "--cert"} {
		if !strings.Contains(said, wanted) {
			t.Errorf("the report does not name %s: %q", wanted, said)
		}
	}
	for _, leaked := range []string{"secret.pdf", "proxypass99", "key.pem"} {
		if strings.Contains(said, leaked) {
			t.Errorf("the report holds %q", leaked)
		}
	}
	if imported.Requests[0].Body.Mode != "formdata" {
		t.Errorf("body = %+v", imported.Requests[0].Body)
	}
}

func TestAPasswordInTheAddressIsHandedToTheGuard(t *testing.T) {
	imported := read(t, `curl https://bob:hunter2hunter2@x.test/a`)

	if !contains(imported.Removed, "hunter2hunter2") {
		t.Errorf("removed = %v", imported.Removed)
	}
}

func TestAnAddressWithoutASchemeIsHttp(t *testing.T) {
	if got := read(t, `curl example.com/a`).Requests[0]; got.URL != "http://example.com/a" || got.Host != "example.com" {
		t.Errorf("request = %+v", got)
	}
}

func TestAnOptionsValueIsNotTakenForAnAddress(t *testing.T) {
	imported := read(t, `curl --unknown-flag something https://x.test/a`)

	if len(imported.Requests) != 1 || !strings.Contains(notes(imported), "not an address") {
		t.Errorf("requests = %d, notes = %q", len(imported.Requests), notes(imported))
	}
}

func TestTextWithoutACurlCommandIsRefused(t *testing.T) {
	for _, text := range []string{"", "   ", "ls -la", "curl", "curl -s"} {
		_, err := NewReader().Read([]byte(text), nil)
		if !errors.Is(err, domain.ErrInvalidCollection) {
			t.Errorf("%q: error = %v", text, err)
		}
	}
}

func TestLimitsAreEnforced(t *testing.T) {
	_, err := NewReader().Read([]byte(strings.Repeat("a", domain.MaxCurlBytes+1)), nil)
	if !errors.Is(err, domain.ErrInvalidCollection) {
		t.Errorf("long text: %v", err)
	}

	many := strings.Repeat("curl https://x.test/a\n", domain.MaxCurlCommands+1)
	if _, err := NewReader().Read([]byte(many), nil); !errors.Is(err, domain.ErrInvalidCollection) {
		t.Errorf("many commands: %v", err)
	}
}

func contains(list []string, wanted string) bool {
	for _, item := range list {
		if item == wanted {
			return true
		}
	}
	return false
}
