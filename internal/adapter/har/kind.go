package har

import (
	"net/url"
	"path"
	"strings"
)

// The kinds a request is sorted into. api is the only one an import ticks by
// default: calls made by script, as opposed to the page and what it loads.
const (
	kindAPI      = "api"
	kindDocument = "document"
	kindScript   = "script"
	kindStyle    = "style"
	kindImage    = "image"
	kindFont     = "font"
	kindMedia    = "media"
	kindOther    = "other"
)

// kindOfResourceType maps the _resourceType a Chromium browser writes.
var kindOfResourceType = map[string]string{
	"xhr":        kindAPI,
	"fetch":      kindAPI,
	"document":   kindDocument,
	"script":     kindScript,
	"stylesheet": kindStyle,
	"image":      kindImage,
	"font":       kindFont,
	"media":      kindMedia,
}

// kindOfExtension is the last resort for a file with neither a resource type nor
// a content type.
var kindOfExtension = map[string]string{
	".js": kindScript, ".mjs": kindScript, ".css": kindStyle,
	".png": kindImage, ".jpg": kindImage, ".jpeg": kindImage, ".gif": kindImage,
	".webp": kindImage, ".svg": kindImage, ".ico": kindImage, ".avif": kindImage,
	".woff": kindFont, ".woff2": kindFont, ".ttf": kindFont, ".otf": kindFont,
	".mp4": kindMedia, ".webm": kindMedia, ".mp3": kindMedia,
	".html": kindDocument, ".htm": kindDocument,
}

// classify says what a browser used a request for.
//
// Chromium names it in _resourceType. Other browsers do not, and the content
// type of the response is read instead, then the extension of the address.
func classify(resourceType, mimeType, method, rawURL string) string {
	if resourceType != "" {
		if kind, known := kindOfResourceType[strings.ToLower(resourceType)]; known {
			return kind
		}
		// preflight, websocket, ping, manifest and the like.
		return kindOther
	}

	if kind := kindOfMime(mimeType); kind != "" {
		return kind
	}

	if address, err := url.Parse(rawURL); err == nil {
		if kind, known := kindOfExtension[strings.ToLower(path.Ext(address.Path))]; known {
			return kind
		}
	}

	// Without a content type, something other than a plain GET was sent by a
	// script: a page does not POST to load itself.
	if strings.ToUpper(method) != "GET" {
		return kindAPI
	}
	return kindOther
}

// kindOfMime sorts a response by its content type, "" when it says nothing.
func kindOfMime(mimeType string) string {
	mime := strings.ToLower(strings.TrimSpace(mimeType))
	mime, _, _ = strings.Cut(mime, ";")

	switch {
	case mime == "":
		return ""
	case strings.Contains(mime, "json"), strings.Contains(mime, "xml") && !strings.Contains(mime, "svg"),
		mime == "text/plain", mime == "application/x-www-form-urlencoded", mime == "application/graphql":
		return kindAPI
	case mime == "text/html", mime == "application/xhtml+xml":
		return kindDocument
	case strings.Contains(mime, "javascript"), strings.Contains(mime, "ecmascript"):
		return kindScript
	case mime == "text/css":
		return kindStyle
	case strings.HasPrefix(mime, "image/"):
		return kindImage
	case strings.HasPrefix(mime, "font/"), strings.Contains(mime, "font"):
		return kindFont
	case strings.HasPrefix(mime, "audio/"), strings.HasPrefix(mime, "video/"):
		return kindMedia
	}
	return ""
}
