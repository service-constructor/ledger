package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/nvsces/ledger/internal/domain"
)

// CreateAccount inserts a client account, or returns the existing one for the
// (user, currency) pair — CreateAccount is idempotent. The caller supplies the
// generated wallet_id, ton_address and memo; on a conflict the stored row wins.
func (r *LedgerRepository) CreateAccount(ctx context.Context, a *domain.Account) (*domain.Account, error) {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO accounts (wallet_id, user_id, ton_address, memo, currency_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (user_id, currency_id) DO NOTHING`,
		a.WalletID, a.UserID, a.TONAddress, a.Memo, a.CurrencyID, a.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert account: %w", err)
	}
	// Return the authoritative row (either the one just inserted, or a pre-existing
	// account for this user+currency).
	return r.getAccountByUser(ctx, a.UserID, a.CurrencyID)
}

func (r *LedgerRepository) getAccountByUser(ctx context.Context, userID string, currencyID int64) (*domain.Account, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT wallet_id, user_id, ton_address, memo, currency_id, created_at
		FROM accounts WHERE user_id = $1 AND currency_id = $2`, userID, currencyID)
	return scanAccount(row)
}

// AccountByMemo resolves the account a deposit belongs to by its memo tag.
func (r *LedgerRepository) AccountByMemo(ctx context.Context, memo string) (*domain.Account, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT wallet_id, user_id, ton_address, memo, currency_id, created_at
		FROM accounts WHERE memo = $1`, memo)
	return scanAccount(row)
}

func scanAccount(row pgx.Row) (*domain.Account, error) {
	var a domain.Account
	err := row.Scan(&a.WalletID, &a.UserID, &a.TONAddress, &a.Memo, &a.CurrencyID, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan account: %w", err)
	}
	return &a, nil
}
