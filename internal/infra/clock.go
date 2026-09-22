// Package infra holds the technical plumbing the layers above depend on:
// the clock, and nothing that carries a business rule.
package infra

import "time"

// SystemClock reads the machine's clock.
//
// It exists so that everything above this layer takes a Clock instead of
// calling time.Now, which is what makes a schedule or a timeout testable.
type SystemClock struct{}

// Now returns the current time, in UTC. Storing anything else invites a run
// recorded in one timezone and read in another.
func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}
