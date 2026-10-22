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
}
