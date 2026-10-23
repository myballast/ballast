// Command migrate applies the schema migrations and exits.
//
// It is what a deployment runs before the new version starts: exit 0 and the
// release goes on, anything else and it stops, with the previous version still
// serving. It reads MIGRATE_DATABASE_URL, the owner role, and nothing else.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/youssefmahersi/ballast/internal/adapter/migrate"
	"github.com/youssefmahersi/ballast/internal/infra/config"
	"github.com/youssefmahersi/ballast/internal/infra/logging"
	"github.com/youssefmahersi/ballast/migrations"
)

func main() {
	log := logging.New("migrate")

	if err := run(log); err != nil {
		log.Error("the migrations did not complete", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.LoadMigrate()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return migrate.Up(ctx, cfg.DatabaseURL, migrations.FS, log)
}
