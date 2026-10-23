package curl

import (
	"fmt"
	"strings"
)

// optionNames maps the short options that matter, or that take a value and so
// must be skipped with it, to their long names.
var shortOptions = map[rune]string{
	'X': "request", 'H': "header", 'd': "data", 'u': "user", 'b': "cookie",
	'A': "user-agent", 'e': "referer", 'F': "form", 'T': "upload-file",
	'o': "output", 'm': "max-time", 'x': "proxy", 'K': "config", 'w': "write-out",
	'c': "cookie-jar", 'E': "cert", 'r': "range", 'D': "dump-header", 'U': "proxy-user",
	'C': "continue-at", 'Y': "speed-limit", 'y': "speed-time", 'z': "time-cond",
	'Q': "quote", 'P': "ftp-port", 't': "telnet-option",
	// Flags: they take no value.
	's': "silent", 'S': "show-error", 'k': "insecure", 'L': "location", 'v': "verbose",
	'i': "include", 'f': "fail", 'N': "no-buffer", 'g': "globoff", 'G': "get", 'I': "head",
	'O': "remote-name", 'J': "remote-header-name", 'j': "junk-session-cookies",
	'4': "ipv4", '6': "ipv6", '0': "http1.0", 'q': "disable", 'Z': "parallel",
}

// takesValue lists the options that read the word after them.
var takesValue = map[string]bool{
	"request": true, "header": true, "data": true, "data-ascii": true, "data-raw": true,
	"data-binary": true, "data-urlencode": true, "url": true, "user": true, "cookie": true,
	"json": true, "form": true, "form-string": true, "upload-file": true, "oauth2-bearer": true,
	"user-agent": true, "referer": true, "output": true, "max-time": true, "connect-timeout": true,
	"retry": true, "retry-delay": true, "retry-max-time": true, "proxy": true, "proxy-user": true,
	"resolve": true, "cacert": true, "cert": true, "key": true, "config": true,
	"write-out": true, "cookie-jar": true, "limit-rate": true, "max-redirs": true, "range": true,
	"dump-header": true, "continue-at": true, "speed-limit": true, "speed-time": true,
	"time-cond": true, "quote": true, "ftp-port": true, "telnet-option": true,
}

// harmless are the options with no effect on the requests a scenario makes: how
// curl prints, follows redirects or retries. They are skipped without a word.
var harmless = map[string]bool{
	"silent": true, "show-error": true, "insecure": true, "location": true, "verbose": true,
	"include": true, "fail": true, "fail-with-body": true, "no-buffer": true, "globoff": true,
	"remote-name": true, "remote-header-name": true, "ipv4": true, "ipv6": true,
	"http1.0": true, "http1.1": true, "http2": true, "http2-prior-knowledge": true,
	"compressed": true, "location-trusted": true, "no-keepalive": true, "tcp-nodelay": true,
	"progress-bar": true, "disable": true, "parallel": true, "junk-session-cookies": true,
	"user-agent": true, "referer": true, "output": true, "max-time": true, "connect-timeout": true,
	"retry": true, "retry-delay": true, "retry-max-time": true, "limit-rate": true,
	"max-redirs": true, "write-out": true, "dump-header": true, "range": true,
}

// reported are the options with a meaning curl gives and a scenario cannot: each
// is named in the report, with the reason, but never with its value.
var reported = map[string]string{
	"proxy":         "a proxy is not used: the load goes out through Ballast's own",
	"proxy-user":    "a proxy is not used: the load goes out through Ballast's own",
	"resolve":       "a name resolved by hand is not used",
	"cacert":        "a certificate file is not read",
	"cert":          "a client certificate is not read",
	"key":           "a client certificate is not read",
	"config":        "a configuration file is not read",
	"cookie-jar":    "a cookie file is not written",
	"upload-file":   "the upload of a file is not converted",
	"form":          "a multipart form is not converted",
	"form-string":   "a multipart form is not converted",
	"continue-at":   "resuming a transfer is not converted",
	"quote":         "this option is for FTP",
	"ftp-port":      "this option is for FTP",
	"telnet-option": "this option is for Telnet",
	"speed-limit":   "this option is not used",
	"speed-time":    "this option is not used",
	"time-cond":     "this option is not used",
}

// option is one option as it was written, with its value when it takes one.
type option struct {
	name  string
	value word
	// given is false for a flag.
	given bool
}

// options splits the words after "curl" into options and positional words, which
// are the addresses. Short options may be clustered (-sSL), glued to their value
// (-XPOST) and long options may be written --name=value.
func options(words []word) (found []option, positional []word, unknown []string) {
	for index := 0; index < len(words); index++ {
		text := words[index].text

		switch {
		case text == "--":
			positional = append(positional, words[index+1:]...)
			return found, positional, unknown

		case strings.HasPrefix(text, "--"):
			name, glued, hasValue := strings.Cut(text[2:], "=")
			canonical := canonicalLong(name)
			if !takesValue[canonical] && !isKnownFlag(canonical) {
				unknown = append(unknown, "--"+shorten(name, 30))
				continue
			}
			entry := option{name: canonical}
			if takesValue[canonical] {
				entry.given = true
				if hasValue {
					entry.value = word{text: glued, expands: words[index].expands}
				} else if index+1 < len(words) {
					index++
					entry.value = words[index]
				}
			}
			found = append(found, entry)

		case strings.HasPrefix(text, "-") && len(text) > 1:
			index = shortCluster(words, index, &found, &unknown)

		default:
			positional = append(positional, words[index])
		}
	}
	return found, positional, unknown
}

// shortCluster reads -abc: flags until one takes a value, which is the rest of
// the word or, when nothing follows it, the next word. It returns the index of
// the last word used.
func shortCluster(words []word, index int, found *[]option, unknown *[]string) int {
	text := []rune(words[index].text)

	for position := 1; position < len(text); position++ {
		name, known := shortOptions[text[position]]
		if !known {
			*unknown = append(*unknown, "-"+string(text[position]))
			continue
		}
		if !takesValue[name] {
			*found = append(*found, option{name: name})
			continue
		}

		entry := option{name: name, given: true}
		if position+1 < len(text) {
			entry.value = word{text: string(text[position+1:]), expands: words[index].expands}
		} else if index+1 < len(words) {
			index++
			entry.value = words[index]
		}
		*found = append(*found, entry)
		return index
	}
	return index
}

// canonicalLong folds the long names that mean the same thing.
func canonicalLong(name string) string {
	switch name {
	case "data-ascii":
		return "data"
	case "form-string":
		return "form"
	case "silent", "quiet":
		return "silent"
	}
	return name
}

// isKnownFlag reports whether a long option without a value is one curl has and
// this reader knows to leave alone, or to act on.
func isKnownFlag(name string) bool {
	switch name {
	case "get", "head", "compressed":
		return true
	}
	return harmless[name]
}

func shorten(text string, limit int) string {
	characters := []rune(text)
	if len(characters) <= limit {
		return text
	}
	return string(characters[:limit])
}

// describe says in a sentence what was not used. The option's name is given,
// its value never.
func describe(name string) string {
	reason, known := reported[name]
	if !known {
		return fmt.Sprintf("the option %s is not used", name)
	}
	return fmt.Sprintf("the option --%s was not used: %s", name, reason)
}
