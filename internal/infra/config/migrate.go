package config

import "errors"

// Migrate is what the migration command reads.
type Migrate struct {
	// DatabaseURL must point at the owner role, the one that owns the tables.
	// It is the opposite of Config.DatabaseURL: the application role has no
	// right to change a table or a policy, and these migrations do both.
	//
	// It is read by this command only. The API and the worker never read it,
	// which matters most where the platform holds one environment per service:
	// the variable sits next to the API's, and nothing in the API looks at it.
	DatabaseURL string
}

// LoadMigrate reads the migration command's configuration.
func LoadMigrate() (Migrate, error) {
	// No default, for the same reason as the application's: a fallback to a
	// local database is a deployment that reports success after migrating the
	// wrong one.
	url := env("MIGRATE_DATABASE_URL", "")
	if url == "" {
		return Migrate{}, errors.New("MIGRATE_DATABASE_URL is not set")
	}

	return Migrate{DatabaseURL: url}, nil
}
