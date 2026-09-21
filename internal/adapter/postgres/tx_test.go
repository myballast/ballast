package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
)

// A run without an organization must be refused before anything is opened.
//
// The pool here is nil on purpose. "Returns an error without opening a
// transaction" is otherwise hard to observe from the outside: a nil pool turns
// the distinction into something the test can see. If the guard sits where it
// belongs, the call returns cleanly. If it ever moves below the line that
// acquires a connection, this panics rather than quietly passing.
func TestInTenantTxRefusesNilOrgWithoutTouchingThePool(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("the pool was reached before the organization was validated: %v", r)
		}
	}()

	db := postgres.NewDB(nil)

	called := false
	err := db.InTenantTx(context.Background(), uuid.Nil, func(pgx.Tx) error {
		called = true
		return nil
	})

	if called {
		t.Error("the callback ran for a nil organization")
	}
	if !errors.Is(err, postgres.ErrNoOrganization) {
		t.Fatalf("got %v, want %v", err, postgres.ErrNoOrganization)
	}
}
