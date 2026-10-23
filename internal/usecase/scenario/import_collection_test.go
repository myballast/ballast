package scenario

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/domain"
)

const aTokenInAFile = "ghp_ThisTokenMustNeverBeLogged0123456789"

func shopCollection() domain.ImportedCollection {
	login := domain.CollectionRequest{
		Position: 0, Name: "Login", Method: "POST", URL: "{{baseUrl}}/login",
		Folders: []string{"Auth"},
		Headers: []domain.CollectionPair{{Name: "Authorization", Value: "Bearer " + aTokenInAFile}},
		Body: domain.CollectionBody{
			Mode: "raw", Language: "json", Raw: `{"email":"a@b.c","password":"` + aTokenInAFile + `"}`,
		},
	}
	list := domain.CollectionRequest{Position: 1, Name: "List", Method: "GET", URL: "{{baseUrl}}/orders?page=1"}

	return domain.ImportedCollection{
		Name:      "Shop",
		Requests:  []domain.CollectionRequest{login, list},
		Variables: []domain.CollectionVariable{{Name: "baseUrl", Value: "https://shop.example.com"}},
	}
}

func collectionFixture(authz Authorizer) (fixture, *fakeCollectionReader) {
	f := newFixture(domain.ScriptReport{}, authz)
	reader := &fakeCollectionReader{collection: shopCollection()}
	f.service.collections = CollectionReaders{Postman: reader, HAR: reader, Curl: reader}
	return f, reader
}

func (f fixture) collectionRequest(positions ...int) CollectionImport {
	return CollectionImport{
		ProjectID: f.projectID, Format: domain.FormatPostman, Name: "shop", Collection: []byte("the file"), Requests: positions,
	}
}

func TestPreviewListsEachRequestWithWhatBecameOfIt(t *testing.T) {
	f, _ := collectionFixture(allowAll{})

	preview, err := f.service.PreviewCollection(context.Background(), f.who, f.projectID, domain.FormatPostman, []byte("c"), nil)
	if err != nil {
		t.Fatalf("PreviewCollection: %v", err)
	}
	if len(preview.Requests) != 2 || preview.Requests[0].Path != "/login" || preview.Requests[1].Path != "/orders" {
		t.Errorf("requests = %+v", preview.Requests)
	}
	if len(preview.Secrets) == 0 {
		t.Error("the secrets in the collection were not listed")
	}
	if f.scenarios.created != nil {
		t.Error("a preview stored a scenario")
	}
}

func TestCollectionPreviewAndImportReadNothingForSomebodyWhoCannotCreateScenarios(t *testing.T) {
	f, reader := collectionFixture(refuseAll{})

	if _, err := f.service.PreviewCollection(context.Background(), f.who, f.projectID, domain.FormatPostman, []byte("c"), nil); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("preview error = %v", err)
	}
	if _, _, _, err := f.service.ImportCollection(context.Background(), f.who, f.collectionRequest(0)); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("import error = %v", err)
	}
	if reader.calls != 0 || f.scenarios.created != nil {
		t.Errorf("read %d times, stored %v", reader.calls, f.scenarios.created)
	}
}

func TestAnImportedCollectionStoresAGraphScenarioWithoutItsSecretsAndReportsThem(t *testing.T) {
	f, _ := collectionFixture(allowAll{})

	created, _, result, err := f.service.ImportCollection(context.Background(), f.who, f.collectionRequest(0, 1))
	if err != nil {
		t.Fatalf("ImportCollection: %v", err)
	}
	if created.Mode != domain.ModeGraph || created.Graph == nil || created.Name != "shop" {
		t.Fatalf("scenario = %+v", created)
	}

	stored := f.scenarios.created
	if stored == nil || stored.Graph == nil {
		t.Fatal("nothing was stored")
	}
	for _, node := range stored.Graph.Nodes {
		if strings.Contains(node.Body+node.Value+node.Path, aTokenInAFile) {
			t.Errorf("the stored graph holds the token: %+v", node)
		}
		for _, header := range node.Headers {
			if strings.Contains(header, aTokenInAFile) {
				t.Errorf("a stored header holds the token: %+v", node.Headers)
			}
		}
	}

	if len(result.Secrets) == 0 {
		t.Error("the secrets to create were not returned")
	}
	for _, secret := range result.Secrets {
		if strings.Contains(secret.Key+secret.Variable, aTokenInAFile) {
			t.Errorf("a secret holds its value: %+v", secret)
		}
	}
}

func TestTheTokenOfAFileIsNeverInTheLogs(t *testing.T) {
	var logs bytes.Buffer
	f, _ := collectionFixture(allowAll{})
	f.service.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	for _, request := range []CollectionImport{f.collectionRequest(0, 1), f.collectionRequest(7)} {
		_, _, _, _ = f.service.ImportCollection(context.Background(), f.who, request)
	}
	_, _ = f.service.PreviewCollection(context.Background(), f.who, f.projectID, domain.FormatPostman, []byte("c"), nil)

	if logs.Len() == 0 {
		t.Fatal("nothing was logged, so this test proves nothing")
	}
	if strings.Contains(logs.String(), aTokenInAFile) {
		t.Errorf("the logs hold the token:\n%s", logs.String())
	}
}

func TestACollectionImportRefusesAChoiceOfNothingAndAMissingRequest(t *testing.T) {
	f, _ := collectionFixture(allowAll{})

	for name, positions := range map[string][]int{"nothing": nil, "a missing request": {0, 9}} {
		_, _, _, err := f.service.ImportCollection(context.Background(), f.who, f.collectionRequest(positions...))
		if !errors.Is(err, domain.ErrInvalidCollection) {
			t.Errorf("%s: error = %v, wanted ErrInvalidCollection", name, err)
		}
	}
	if f.scenarios.created != nil {
		t.Error("a scenario was stored from a refused choice")
	}
}

func TestACollectionImportPassesOnWhatTheReaderRefused(t *testing.T) {
	f, reader := collectionFixture(allowAll{})
	reader.err = domain.ErrInvalidCollection

	_, _, _, err := f.service.ImportCollection(context.Background(), f.who, f.collectionRequest(0))
	if !errors.Is(err, domain.ErrInvalidCollection) || f.scenarios.created != nil {
		t.Fatalf("error = %v, stored = %v", err, f.scenarios.created)
	}
}

func TestACollectionOfRequestsThatCannotBeMadeStoresNothing(t *testing.T) {
	f, reader := collectionFixture(allowAll{})
	reader.collection.Requests = []domain.CollectionRequest{{Position: 0, Name: "Preflight", Method: "OPTIONS", URL: "/x"}}

	_, _, _, err := f.service.ImportCollection(context.Background(), f.who, f.collectionRequest(0))
	if !errors.Is(err, domain.ErrInvalidCollection) || f.scenarios.created != nil {
		t.Fatalf("error = %v, stored = %v", err, f.scenarios.created)
	}
}

func TestEachFormatIsReadByItsOwnReader(t *testing.T) {
	f := newFixture(domain.ScriptReport{}, allowAll{})
	postman := &fakeCollectionReader{collection: shopCollection()}
	har := &fakeCollectionReader{collection: shopCollection()}
	curl := &fakeCollectionReader{collection: shopCollection()}
	f.service.collections = CollectionReaders{Postman: postman, HAR: har, Curl: curl}

	for format, reader := range map[domain.CollectionFormat]*fakeCollectionReader{
		domain.FormatPostman: postman, domain.FormatHAR: har, domain.FormatCurl: curl,
	} {
		if _, err := f.service.PreviewCollection(context.Background(), f.who, f.projectID, format, []byte("c"), nil); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if reader.calls != 1 {
			t.Errorf("%s: its reader was called %d times", format, reader.calls)
		}
	}
	if postman.calls != 1 || har.calls != 1 || curl.calls != 1 {
		t.Errorf("calls = %d %d %d, wanted one each", postman.calls, har.calls, curl.calls)
	}
}

func TestAFormatWithoutAReaderIsRefusedBeforeAnythingIsRead(t *testing.T) {
	f, reader := collectionFixture(allowAll{})

	for _, format := range []domain.CollectionFormat{"", "xml"} {
		_, err := f.service.PreviewCollection(context.Background(), f.who, f.projectID, format, []byte("c"), nil)
		if !errors.Is(err, domain.ErrInvalidCollection) {
			t.Errorf("preview %q: error = %v", format, err)
		}

		request := f.collectionRequest(0)
		request.Format = format
		if _, _, _, err := f.service.ImportCollection(context.Background(), f.who, request); !errors.Is(err, domain.ErrInvalidCollection) {
			t.Errorf("import %q: error = %v", format, err)
		}
	}
	if reader.calls != 0 || f.scenarios.created != nil {
		t.Errorf("read %d times, stored %v", reader.calls, f.scenarios.created)
	}
}
