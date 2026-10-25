package domain

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// MaxArtifactNameLength bounds the last segment of an object key. A constant,
// like the other limits of this package: nothing a caller sends should be able
// to move it.
const MaxArtifactNameLength = 128

// ArtifactKey is where an artifact of a run is kept in the object store:
//
//	org/<organization>/run/<run>/<name>
//
// It is the only place a key is built. The object store has one bucket and one
// set of credentials for every organization, so the prefix is what keeps two
// customers' files apart — which makes this function the isolation, not a
// naming convention. A name that could climb out of the prefix, or turn into
// another key once a client or a proxy has read it, is refused rather than
// cleaned: the callers pick names from a short list of their own, so a refusal
// is a bug that should be loud.
func ArtifactKey(orgID, runID uuid.UUID, name string) (string, error) {
	if orgID == uuid.Nil {
		return "", errors.New("an artifact needs an organization")
	}
	if runID == uuid.Nil {
		return "", errors.New("an artifact needs a run")
	}
	if err := validateArtifactName(name); err != nil {
		return "", err
	}

	return "org/" + orgID.String() + "/run/" + runID.String() + "/" + name, nil
}

// validateArtifactName accepts lower-case letters, digits, '-', '_' and '.',
// and nothing else: no separator, no escape, no space, no control character.
// It may not start with a dot, and may not contain two in a row.
func validateArtifactName(name string) error {
	if name == "" {
		return errors.New("an artifact needs a name")
	}
	if len(name) > MaxArtifactNameLength {
		return fmt.Errorf("an artifact name is at most %d characters", MaxArtifactNameLength)
	}
	if name[0] == '.' {
		return errors.New("an artifact name does not start with a dot")
	}

	previousDot := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		isDot := c == '.'

		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		case isDot && !previousDot:
		default:
			return errors.New("an artifact name holds lower-case letters, digits, '-', '_' and single dots")
		}
		previousDot = isDot
	}

	return nil
}
