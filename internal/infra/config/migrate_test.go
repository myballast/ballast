package config

import "testing"

func TestLoadMigrateRefusesToGuessTheDatabase(t *testing.T) {
	t.Setenv("MIGRATE_DATABASE_URL", "")

	if _, err := LoadMigrate(); err == nil {
		t.Fatal("LoadMigrate fell back to a database nobody named")
	}
}

func TestLoadMigrateReadsTheOwnerURLAndNotTheApplicationsOne(t *testing.T) {
	t.Setenv("MIGRATE_DATABASE_URL", "postgres://owner@h/d")
	t.Setenv("BALLAST_DATABASE_URL", "postgres://ballast_app@h/d")

	cfg, err := LoadMigrate()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://owner@h/d" {
		t.Errorf("DatabaseURL = %q, want the owner's", cfg.DatabaseURL)
	}
}
