// Package repository contains the repository interfaces and their PostgreSQL
// implementations. Each repository is responsible for exactly one persistence
// concern and accepts a pgx transaction so that callers can compose multiple
// operations into a single atomic unit.
package repository
