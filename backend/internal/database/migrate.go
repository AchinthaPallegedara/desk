package database

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for goose
	"github.com/pressly/goose/v3"

	"github.com/claviq/desk/backend/migrations"
)

// Migrate runs goose against the embedded SQL migrations.
// command: up | down | status | redo | version
//
// It must use the schema-owner connection, not the app role.
func Migrate(dsn, command string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	switch command {
	case "up":
		return goose.Up(db, ".")
	case "down":
		return goose.Down(db, ".")
	case "redo":
		return goose.Redo(db, ".")
	case "status":
		return goose.Status(db, ".")
	case "version":
		return goose.Version(db, ".")
	default:
		return fmt.Errorf("unknown migrate command %q (use up|down|redo|status|version)", command)
	}
}
