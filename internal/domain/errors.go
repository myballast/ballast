package domain

import "errors"

// ErrInvalidProfileParams reports load parameters that describe no runnable
// test. Wrap it with what exactly is wrong; callers match on the sentinel.
var ErrInvalidProfileParams = errors.New("invalid profile parameters")

// ErrNotFound reports that nothing with the given identity exists — or that
// nothing the caller's organization is allowed to see does, which from the
// outside is the same thing and must stay that way.
var ErrNotFound = errors.New("not found")

// ErrTargetNotVerified reports an attempt to send load at an environment whose
// ownership was never proved and approved. It is the control that keeps Ballast
// from being pointed at somebody else's servers.
var ErrTargetNotVerified = errors.New("target environment is not verified")

// ErrCrossProject reports identifiers that do not belong together. Row-level
// security keeps one organization's rows away from another's; it says nothing
// about a scenario and an environment from two different projects of the same
// organization.
var ErrCrossProject = errors.New("scenario and environment belong to different projects")

// ErrForbidden reports a request the caller's permissions do not allow. It
// says nothing about whether the thing asked for exists: for somebody with no
// business there, the two answers must look the same.
var ErrForbidden = errors.New("forbidden")

// ErrLastAdministrator reports a change that would leave a project with nobody
// able to administer it. Recovering from that takes a hand on the database.
var ErrLastAdministrator = errors.New("the project would be left with no administrator")
