package openapi

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// resolver follows $ref inside the file, and nowhere else.
//
// It counts every reference it meets, and it follows a chain of references only
// so far. The only way out of those bounds is an error, so a file built to make
// the reader loop or fan out gets a refusal instead of a stalled server.
type resolver struct {
	root          map[string]any
	referencesMet int
}

// resolve returns the value a reference ends at, or the value itself when it is
// not a reference.
//
// A chain such as A pointing at B pointing at C is followed to the end. A chain
// that comes back to a reference it already passed through can never end, and is
// refused as circular.
func (r *resolver) resolve(value any) (any, error) {
	passedThrough := map[string]bool{}

	for hops := 0; ; hops++ {
		object := asMap(value)
		target, isReference := object["$ref"].(string)
		if !isReference {
			return value, nil
		}

		r.referencesMet++
		if r.referencesMet > domain.MaxSpecRefs {
			return nil, fmt.Errorf("%w: the file holds more than %d $ref, the most Ballast reads",
				domain.ErrInvalidSpec, domain.MaxSpecRefs)
		}
		if hops >= domain.MaxSpecRefDepth {
			return nil, fmt.Errorf("%w: a chain of $ref is longer than %d, the most Ballast follows",
				domain.ErrInvalidSpec, domain.MaxSpecRefDepth)
		}
		if passedThrough[target] {
			return nil, fmt.Errorf("%w: the $ref %q is circular, it leads back to itself",
				domain.ErrInvalidSpec, shortened(target, 100))
		}
		passedThrough[target] = true

		found, err := r.lookup(target)
		if err != nil {
			return nil, err
		}
		value = found
	}
}

// resolveMap resolves a value that must be an object, and returns nil for
// anything else.
func (r *resolver) resolveMap(value any) (map[string]any, error) {
	resolved, err := r.resolve(value)
	if err != nil {
		return nil, err
	}
	return asMap(resolved), nil
}

// lookup finds what a JSON pointer such as #/components/parameters/Id points at
// in the same file.
func (r *resolver) lookup(target string) (any, error) {
	if !strings.HasPrefix(target, "#/") {
		return nil, fmt.Errorf("%w: the $ref %q points outside the file, and Ballast does not follow it",
			domain.ErrInvalidSpec, shortened(target, 100))
	}

	var current any = r.root
	for _, rawStep := range strings.Split(target[2:], "/") {
		step, err := pointerStep(rawStep)
		if err != nil {
			return nil, fmt.Errorf("%w: the $ref %q is not a valid pointer", domain.ErrInvalidSpec, shortened(target, 100))
		}

		current = child(current, step)
		if current == nil {
			return nil, fmt.Errorf("%w: the $ref %q points at nothing", domain.ErrInvalidSpec, shortened(target, 100))
		}
	}

	return current, nil
}

// pointerStep undoes the escaping of one piece of a JSON pointer: percent
// encoding first, then ~1 for a slash and ~0 for a tilde.
func pointerStep(raw string) (string, error) {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", err
	}
	decoded = strings.ReplaceAll(decoded, "~1", "/")
	decoded = strings.ReplaceAll(decoded, "~0", "~")
	return decoded, nil
}

// child steps into an object by key or into a list by position.
func child(parent any, step string) any {
	if object := asMap(parent); object != nil {
		return object[step]
	}

	if list := asList(parent); list != nil {
		position, err := strconv.Atoi(step)
		if err != nil || position < 0 || position >= len(list) {
			return nil
		}
		return list[position]
	}

	return nil
}
