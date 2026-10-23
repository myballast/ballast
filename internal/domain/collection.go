package domain

import "errors"

// ErrInvalidCollection reports a collection Ballast refuses to read or to turn
// into a scenario. The message never repeats a value taken from the file.
var ErrInvalidCollection = errors.New("invalid collection")

// Limits on what a collection import reads. Constants of domain, like the
// limits on a specification: the file is chosen by whoever uploads it.
const (
	// MaxCollectionBytes is the largest collection, or environment file,
	// accepted.
	MaxCollectionBytes = 5 * 1024 * 1024
	// MaxCollectionDepth is how many folders may be nested.
	MaxCollectionDepth = 10
	// MaxCollectionRequests is how many requests one collection may hold.
	MaxCollectionRequests = 5000

	// MaxHARBytes is the largest HAR file accepted. A browser records every
	// response body in the file, so a few hundred requests are often tens of
	// megabytes; the bodies are skipped while reading and never kept.
	MaxHARBytes = 25 * 1024 * 1024
	// MaxHAREntries is how many requests one HAR file may hold.
	MaxHAREntries = 5000
	// MaxCurlBytes is the longest text of cURL commands accepted.
	MaxCurlBytes = 256 * 1024
	// MaxCurlCommands is how many commands one pasted text may hold.
	MaxCurlCommands = 200
)

// CollectionFormat is what a file of requests was exported from. The formats
// differ in how they are read; once read, they are the same
// ImportedCollection and go through the same analysis.
type CollectionFormat string

// The formats of requests that can be imported.
const (
	FormatPostman CollectionFormat = "postman"
	FormatHAR     CollectionFormat = "har"
	FormatCurl    CollectionFormat = "curl"
)

// CollectionPair is one name and value: a header, or a field of a form body.
type CollectionPair struct {
	Name  string
	Value string
}

// CollectionBody is the body of a request, as the collection wrote it.
type CollectionBody struct {
	// Mode is raw, urlencoded, formdata, file or graphql; empty for no body.
	Mode string
	// Raw is the text of a raw body, and Language what the collection says it
	// is written in: json, text, xml.
	Raw      string
	Language string
	// Pairs are the fields of an urlencoded body.
	Pairs []CollectionPair
}

// CollectionAuth is the authentication a request ends up with, after folders
// and the collection have been asked: the reader resolves the inheritance.
type CollectionAuth struct {
	// Type is bearer, apikey, basic, oauth2, and so on.
	Type   string
	Params map[string]string
}

// CollectionScript is a script the collection attaches to a request. Only its
// kind and size are kept: Ballast does not run it, and says so.
type CollectionScript struct {
	// Kind is "prerequest" or "test".
	Kind  string
	Lines int
}

// CollectionRequest is one request of a collection, in the order the collection
// walks them.
type CollectionRequest struct {
	// Position is the request's rank in the walk, from zero. It is how a caller
	// names a request: names repeat, positions do not.
	Position int
	// Folders is the path of folders the request sits in, outermost first.
	Folders []string
	Name    string
	Method  string
	// URL is as written, with its {{variables}}, :path variables and query.
	URL string
	// PathVariables are the values the collection gives to :name in the path.
	PathVariables map[string]string
	Headers       []CollectionPair
	Body          CollectionBody
	Auth          *CollectionAuth
	Scripts       []CollectionScript
	// Host is the host the request addresses, when the reader knows it. A HAR
	// file holds absolute addresses, and the choice screen filters on them.
	Host string
	// Kind says what the browser used the request for: api, document, script,
	// style, image, font, media or other. Empty for a format that does not say.
	Kind string
	// Notes are things the reader changed in this request, such as a cookie it
	// removed. They become findings tied to the request.
	Notes []string
}

// CollectionVariable is a variable of the collection or of an environment file.
type CollectionVariable struct {
	Name  string
	Value string
	// Secret is true when the environment file says so.
	Secret bool
}

// ImportFinding is one thing the import could not carry over, or changed, said
// in a sentence and tied to the request it is about. A finding is how an import
// avoids ignoring anything silently.
type ImportFinding struct {
	// Request is the name of the request, empty for the collection itself.
	Request string
	Message string
}

// ImportedCollection is everything an import reads from a collection and an
// optional environment file.
type ImportedCollection struct {
	Name      string
	Requests  []CollectionRequest
	Variables []CollectionVariable
	// Findings are what the reader itself left out, such as a script on a folder.
	Findings []ImportFinding
	// ManyHosts is true when the requests legitimately address several hosts and
	// the person picks one, as in a HAR recorded on a page that calls its API and
	// its analytics. The preview then lists every request; an import still sends
	// its load to one target only.
	ManyHosts bool
	// Removed are values the reader took out of the file on purpose, such as the
	// cookies of a HAR. They are never put in a graph; the final check refuses an
	// import that still holds one, wherever it turned up.
	Removed []string
}
