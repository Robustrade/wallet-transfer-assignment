package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Migrate(ctx context.Context, db *DB) error {
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}

	for _, file := range files {
		content, err := migrations.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}

		for _, stmt := range splitStatements(string(content)) {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("apply migration %s: %w", file, err)
			}
		}
	}
	return nil
}

func splitStatements(sql string) []string {
	var stripped strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if code, _, found := strings.Cut(line, "--"); found {
			line = code
		}
		stripped.WriteString(line)
		stripped.WriteString("\n")
	}

	var statements []string
	for _, stmt := range strings.Split(stripped.String(), ";") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			statements = append(statements, stmt)
		}
	}
	return statements
}
