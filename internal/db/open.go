package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens (creating if needed) the SQLite database at path and applies migrations.
func Open(path string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	if err := Migrate(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// OpenExisting opens the database at path without creating or migrating it,
// for tools (like backup) that run next to a live server.
func OpenExisting(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return sql.Open("sqlite", dsn(path))
}

func dsn(path string) string {
	return fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
}

func Migrate(conn *sql.DB) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(log.New(io.Discard, "", 0))
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	if err := goose.Up(conn, "migrations"); err != nil {
		return fmt.Errorf("migrating database: %w", err)
	}
	return nil
}
