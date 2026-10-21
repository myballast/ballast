package openapi

import (
	"fmt"
	"sort"
)

// The decoded file is a tree of map[string]any, []any and scalars. These
// helpers read it without ever panicking on a shape nobody expected: a value of
// the wrong kind is simply absent.

func asMap(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func asList(value any) []any {
	list, _ := value.([]any)
	return list
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

func sortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// scalarText writes a number, a boolean or a string as text, and says whether
// the value was one of those. An object or a list is not a header value.
func scalarText(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool, int, int64, uint64, float64:
		return fmt.Sprint(typed), true
	case fmt.Stringer:
		// json.Number
		return typed.String(), true
	}
	return "", false
}

// shortened cuts text to at most limit characters, for a title or a summary
// that will be shown in a list.
func shortened(text string, limit int) string {
	characters := []rune(text)
	if len(characters) <= limit {
		return text
	}
	return string(characters[:limit])
}
