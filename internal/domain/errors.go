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

// ErrInvalidScenario reports a scenario Ballast will not store, for a reason
// that has nothing to do with its script.
var ErrInvalidScenario = errors.New("invalid scenario")

// ErrInvalidThreshold reports a threshold rule Ballast will not store.
var ErrInvalidThreshold = errors.New("invalid threshold")

// ErrScenarioNotEmpty reports an attempt to delete a scenario that still holds
// runs. Its rows cascade, so deleting it would take their history with it.
var ErrScenarioNotEmpty = errors.New("the scenario still holds runs")

// ErrInvalidEnvironment reports an environment Ballast will not store. It says
// nothing about whether anybody owns the target: that is the verification's
// answer to give.
var ErrInvalidEnvironment = errors.New("invalid environment")

// ErrInvalidSecret reports a secret Ballast will not store. Nothing wrapping it
// ever repeats the value.
var ErrInvalidSecret = errors.New("invalid secret")

// ErrEnvironmentNotEmpty reports an attempt to delete an environment that still
// holds runs. Their rows cascade, so deleting it would take the history of
// everything ever measured against that target with it.
var ErrEnvironmentNotEmpty = errors.New("the environment still holds runs")

// ErrOutsideAllowedWindow reports a run asked for outside the hours its target
// tolerates load. It is checked when the run is queued and again before the
// generator starts, because a run can wait in the queue until the window has
// closed behind it.
var ErrOutsideAllowedWindow = errors.New("the environment accepts no load at this hour")
