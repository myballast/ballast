package postman

import "fmt"

// The decoded file is a tree of map[string]any, []any and scalars. These helpers
// read it without ever panicking on a shape nobody expected: a value of the wrong
// kind is simply absent.

func asObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func objectAt(object map[string]any, key string) map[string]any {
	return asObject(object[key])
}

func arrayAt(object map[string]any, key string) []any {
	list, _ := object[key].([]any)
	return list
}

// stringAt reads a string, or the text of a number or a boolean.
func stringAt(object map[string]any, key string) string {
	switch typed := object[key].(type) {
	case string:
		return typed
	case bool, fmt.Stringer:
		return fmt.Sprint(typed)
	}
	return ""
}

func boolAt(object map[string]any, key string) bool {
	flag, _ := object[key].(bool)
	return flag
}

// shortened cuts text to at most limit characters.
func shortened(text string, limit int) string {
	characters := []rune(text)
	if len(characters) <= limit {
		return text
	}
	return string(characters[:limit])
}
