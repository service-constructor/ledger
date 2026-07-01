package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// isCheckViolation reports whether err is a Postgres CHECK constraint violation
// (SQLSTATE 23514) for the named constraint. Used to turn the wallet_balances
// non-negative CHECK into a domain-level insufficient-funds error.
func isCheckViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23514" && pgErr.ConstraintName == constraint
	}
	return false
}
