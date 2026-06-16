package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatabaseURLFromParts(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "postgres")
	t.Setenv("DB_PASSWORD", "postgres")
	t.Setenv("DB_NAME", "rapido_db")
	t.Setenv("DB_SSLMODE", "disable")

	got, err := databaseURL()
	require.NoError(t, err)
	assert.Equal(t, "postgres://postgres:postgres@localhost:5432/rapido_db?sslmode=disable", got)
}

func TestDatabaseURLExplicitWins(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://custom:secret@db:5432/app?sslmode=require")
	t.Setenv("DB_HOST", "ignored")

	got, err := databaseURL()
	require.NoError(t, err)
	assert.Equal(t, "postgres://custom:secret@db:5432/app?sslmode=require", got)
}
