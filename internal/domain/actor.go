package domain

import "github.com/google/uuid"

// Actor is who is behind an action: a person, or an API token, or nobody.
//
// The two never travel together. A run and a journal entry each say which one
// they were done by, and a token is never written down as nobody: an entry with
// no author means Ballast itself acted, and a credential somebody holds must not
// be mistaken for that.
type Actor struct {
	UserID  *uuid.UUID
	TokenID *uuid.UUID
}

// IsZero reports whether nobody is behind the action.
func (a Actor) IsZero() bool {
	return a.UserID == nil && a.TokenID == nil
}

// Actor is who this identity acts as. An identity that is both a person and a
// token, or neither, is nobody: resolving it to whichever is read first would be
// a guess, and the journal is where a guess does harm.
//
// What comes back is a copy: it does not follow a later change of the identity.
func (i Identity) Actor() Actor {
	person, token := i.UserID != uuid.Nil, i.TokenID != uuid.Nil

	switch {
	case person && !token:
		id := i.UserID
		return Actor{UserID: &id}
	case token && !person:
		id := i.TokenID
		return Actor{TokenID: &id}
	}
	return Actor{}
}
