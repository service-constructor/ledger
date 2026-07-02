package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nvsces/ledger/internal/domain"
)

// ListCurrencies returns all known currencies, ordered by id.
func (r *LedgerRepository) ListCurrencies(ctx context.Context) ([]*domain.Currency, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, name, symbol, decimals, is_real
		FROM currencies ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list currencies: %w", err)
	}
	defer rows.Close()

	var out []*domain.Currency
	for rows.Next() {
		var c domain.Currency
		if err := rows.Scan(&c.ID, &c.Code, &c.Name, &c.Symbol, &c.Decimals, &c.IsReal); err != nil {
			return nil, fmt.Errorf("scan currency: %w", err)
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// GetCurrency returns one currency by id, or ErrCurrencyNotFound.
func (r *LedgerRepository) GetCurrency(ctx context.Context, id int64) (*domain.Currency, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, code, name, symbol, decimals, is_real
		FROM currencies WHERE id = $1`, id)
	var c domain.Currency
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.Symbol, &c.Decimals, &c.IsReal)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrCurrencyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get currency: %w", err)
	}
	return &c, nil
}
