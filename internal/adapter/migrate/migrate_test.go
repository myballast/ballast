package migrate

import (
	"strings"
	"testing"
)

func TestDatabaseURLNamesTheDriverAndKeepsTheBookkeepingOutOfPublic(t *testing.T) {
	cases := map[string]string{
		"postgres://owner:pw@db.example.test:5432/railway":                   "pgx5://owner:pw@db.example.test:5432/railway",
		"postgresql://owner:pw@db.example.test:5432/railway?sslmode=require": "pgx5://owner:pw@db.example.test:5432/railway",
	}

	for in, wantPrefix := range cases {
		got, err := databaseURL(in)
		if err != nil {
			t.Fatalf("databaseURL(%q): %v", in, err)
		}
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("databaseURL(%q) = %q, want it to start with %q", in, got, wantPrefix)
		}
		// The bookkeeping table lives in its own schema: the application role
		// cannot see it, and the schema tests allow only tenant tables in public.
		if !strings.Contains(got, "x-migrations-table-quoted=1") ||
			!strings.Contains(got, "x-migrations-table=%22migrate%22.%22schema_migrations%22") {
			t.Errorf("databaseURL(%q) = %q, want the migrate.schema_migrations table", in, got)
		}
	}
}

func TestDatabaseURLKeepsTheQueryItWasGiven(t *testing.T) {
	got, err := databaseURL("postgres://o:p@h:1/d?sslmode=require&connect_timeout=5")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sslmode=require", "connect_timeout=5"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lost %q", got, want)
		}
	}
}

func TestDatabaseURLRefusesWhatIsNotPostgresWithoutEchoingIt(t *testing.T) {
	const secret = "s3cr3t-password"

	for _, in := range []string{
		"mysql://u:" + secret + "@h/d",
		"http://u:" + secret + "@h/d",
		"://u:" + secret + "@h/d",
		"",
	} {
		_, err := databaseURL(in)
		if err == nil {
			t.Errorf("databaseURL(%q) was accepted", in)
			continue
		}
		// The URL carries the owner's password, and this error ends up in a
		// deployment log.
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the error for %q repeats the password: %v", in, err)
		}
	}
}
