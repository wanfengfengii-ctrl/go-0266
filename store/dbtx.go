package store

import (
	"context"
	"database/sql"
)

// dbtx is the narrow query/exec surface shared by *sql.DB and *sql.Tx. It lets
// the repository helpers participate either in a caller's transaction or in an
// auto-commit context without duplicating SQL.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// withTx runs fn inside a single SQLite write transaction and rolls back on any
// error so a failed write can never leave partial samples, leases, coverage
// cells, reveal records or final decisions (failure boundary 1).
func (s *SQLite) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
