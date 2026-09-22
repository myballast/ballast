// Package e2e holds the test that exercises the whole chain: an HTTP request
// in, a container run, and metrics in the database.
//
// It sits outside internal/domain, internal/usecase and internal/adapter on
// purpose. The dependency rule says a use case never imports an adapter, and
// this test has to wire all of them together — putting it inside any layer
// would either break that rule or hide the wiring somewhere it does not belong.
// There is nothing here but tests.
package e2e
