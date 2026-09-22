package domain

import "errors"

// ErrInvalidProfileParams reports load parameters that describe no runnable
// test. Wrap it with what exactly is wrong; callers match on the sentinel.
var ErrInvalidProfileParams = errors.New("invalid profile parameters")
