package storage

import (
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

var embedMigrations embed.FS

const (
	migrationsDir = "migrations"
)

func (s *Storage) Migrate() error {
	goose.SetBaseFS(embedMigrations)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}

	if err := goose.Up(s.db, migrationsDir); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}
