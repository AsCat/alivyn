package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

// Probe verifies that the pure-Go SQLite driver can create, write, and read
// a local database. It does not touch Alivyn user data.
func Probe(ctx context.Context) error {
	file, err := os.CreateTemp("", "alivyn-sqlite-probe-*.db")
	if err != nil {
		return err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		os.Remove(path)
		return err
	}
	defer os.Remove(path)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, "CREATE TABLE probe (value TEXT NOT NULL)"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO probe (value) VALUES (?)", "alivyn"); err != nil {
		return err
	}
	var value string
	if err := db.QueryRowContext(ctx, "SELECT value FROM probe LIMIT 1").Scan(&value); err != nil {
		return err
	}
	if value != "alivyn" {
		return fmt.Errorf("unexpected value %q", value)
	}
	return nil
}
