package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/nvsces/ledger/internal/domain"
)

// LedgerRepository applies double-entry operations against Postgres. Every
// operation runs in one serializable-by-locking transaction: it inserts a
// balanced set of journal entries and updates the materialized balances
// atomically. Idempotency is enforced by the entries_order_op_uniq index — a
// replay inserts nothing and reports applied=false.
type LedgerRepository struct {
	pool *pgxpool.Pool
}

// NewLedgerRepository wraps a pgx pool.
func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{pool: pool}
}

// leg is one side of a posting: move `amount` (a positive magnitude) into
// (wallet, bucket). Sign is applied by the caller when building a balanced set.
type leg struct {
	walletID string
	bucket   domain.Bucket
	amount   decimal.Decimal // signed
}

// Freeze moves amount from the wallet's AVAILABLE bucket into HELD.
func (r *LedgerRepository) Freeze(ctx context.Context, orderID, walletID string, currencyID int64, amount decimal.Decimal) (bool, error) {
	return r.apply(ctx, orderID, domain.OpFreeze, currencyID, []leg{
		{walletID, domain.BucketAvailable, amount.Neg()},
		{walletID, domain.BucketHeld, amount},
	})
}

// Capture debits the wallet's HELD bucket and credits the receiving wallet (net)
// and platform wallet (fee) AVAILABLE buckets. net+fee is debited from held.
func (r *LedgerRepository) Capture(ctx context.Context, orderID, walletID, receivingWalletID, platformWalletID string, currencyID int64, net, fee decimal.Decimal) (bool, error) {
	return r.apply(ctx, orderID, domain.OpCapture, currencyID, []leg{
		{walletID, domain.BucketHeld, net.Add(fee).Neg()},
		{receivingWalletID, domain.BucketAvailable, net},
		{platformWalletID, domain.BucketAvailable, fee},
	})
}

// Release returns the wallet's HELD funds for this order to AVAILABLE. The held
// magnitude is looked up from the FREEZE entry so the caller need not resupply it.
func (r *LedgerRepository) Release(ctx context.Context, orderID, walletID string, currencyID int64) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Idempotency guard first: if RELEASE already applied, no-op.
	applied, err := alreadyApplied(ctx, tx, orderID, domain.OpRelease)
	if err != nil {
		return false, err
	}
	if applied {
		return false, tx.Commit(ctx)
	}

	// Recover the held magnitude from the FREEZE posting for this order.
	var held decimal.Decimal
	err = tx.QueryRow(ctx, `
		SELECT amount FROM entries
		WHERE order_id = $1 AND op = 'FREEZE' AND bucket = 'HELD' AND wallet_id = $2`,
		orderID, walletID).Scan(&held)
	if errors.Is(err, pgx.ErrNoRows) {
		// Nothing was frozen for this order/wallet; releasing is a no-op.
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, fmt.Errorf("lookup freeze: %w", err)
	}

	legs := []leg{
		{walletID, domain.BucketHeld, held.Neg()},
		{walletID, domain.BucketAvailable, held},
	}
	if err := postLegs(ctx, tx, orderID, domain.OpRelease, currencyID, legs); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// Deposit credits external funds into a wallet's AVAILABLE bucket. It is a
// single unbalanced credit (money enters the system from outside), idempotent on
// ref: the ref is stored as the entry's order_id, so a replayed deposit inserts
// nothing and reports applied=false.
func (r *LedgerRepository) Deposit(ctx context.Context, ref, walletID string, currencyID int64, amount decimal.Decimal) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	applied, err := alreadyApplied(ctx, tx, ref, domain.OpDeposit)
	if err != nil {
		return false, err
	}
	if applied {
		return false, tx.Commit(ctx) // replay: no-op
	}

	legs := []leg{{walletID, domain.BucketAvailable, amount}}
	if err := postLegs(ctx, tx, ref, domain.OpDeposit, currencyID, legs); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// apply runs one operation: idempotency check, post the balanced legs, update
// balances — all in a single transaction.
func (r *LedgerRepository) apply(ctx context.Context, orderID string, op domain.Op, currencyID int64, legs []leg) (bool, error) {
	if err := assertBalanced(legs); err != nil {
		return false, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	applied, err := alreadyApplied(ctx, tx, orderID, op)
	if err != nil {
		return false, err
	}
	if applied {
		return false, tx.Commit(ctx) // replay: no-op
	}

	if err := postLegs(ctx, tx, orderID, op, currencyID, legs); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// alreadyApplied reports whether any entry exists for (order, op).
func alreadyApplied(ctx context.Context, tx pgx.Tx, orderID string, op domain.Op) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM entries WHERE order_id = $1 AND op = $2)`,
		orderID, string(op)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("idempotency check: %w", err)
	}
	return exists, nil
}

// postLegs inserts the journal rows and folds them into wallet_balances. The
// NOT NULL + nonneg constraints reject any negative bucket, surfacing as
// ErrInsufficient.
func postLegs(ctx context.Context, tx pgx.Tx, orderID string, op domain.Op, currencyID int64, legs []leg) error {
	for _, l := range legs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO entries (order_id, op, wallet_id, bucket, currency_id, amount)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			orderID, string(op), l.walletID, string(l.bucket), currencyID, l.amount,
		); err != nil {
			return fmt.Errorf("insert entry: %w", err)
		}
		if err := bumpBalance(ctx, tx, l, currencyID); err != nil {
			return err
		}
	}
	return nil
}

// bumpBalance applies a signed leg to the wallet's materialized balance. It
// first ensures a zero row exists (a no-op if it does), then applies the delta
// with a plain UPDATE — so the non-negative CHECK is evaluated only on the final
// summed value, never on an intermediate negative INSERT candidate. A resulting
// negative bucket violates the CHECK and is mapped to ErrInsufficient.
func bumpBalance(ctx context.Context, tx pgx.Tx, l leg, currencyID int64) error {
	col := "available"
	if l.bucket == domain.BucketHeld {
		col = "held"
	}
	// Ensure the (wallet, currency) row exists at zero. DO NOTHING keeps any
	// existing balance untouched.
	if _, err := tx.Exec(ctx, `
		INSERT INTO wallet_balances (wallet_id, currency_id, available, held)
		VALUES ($1,$2,0,0) ON CONFLICT (wallet_id, currency_id) DO NOTHING`,
		l.walletID, currencyID); err != nil {
		return fmt.Errorf("ensure balance row: %w", err)
	}
	// Apply the delta; the CHECK evaluates on the final value.
	sql := fmt.Sprintf(
		`UPDATE wallet_balances SET %[1]s = %[1]s + $3, updated_at = now()
		 WHERE wallet_id = $1 AND currency_id = $2`, col)
	if _, err := tx.Exec(ctx, sql, l.walletID, currencyID, l.amount); err != nil {
		if isCheckViolation(err, "wallet_balances_nonneg") {
			return fmt.Errorf("%w: wallet %s bucket %s", domain.ErrInsufficient, l.walletID, l.bucket)
		}
		return fmt.Errorf("update balance: %w", err)
	}
	return nil
}

// assertBalanced verifies the signed legs sum to zero — the double-entry
// invariant. A non-zero sum is a programming error, not a runtime condition.
func assertBalanced(legs []leg) error {
	sum := decimal.Zero
	for _, l := range legs {
		sum = sum.Add(l.amount)
	}
	if !sum.IsZero() {
		return fmt.Errorf("unbalanced posting: legs sum to %s, want 0", sum)
	}
	return nil
}

// GetBalance returns a wallet's derived available/held for a currency. A wallet
// with no entries reads as zero/zero.
func (r *LedgerRepository) GetBalance(ctx context.Context, walletID string, currencyID int64) (*domain.Balance, error) {
	var avail, held decimal.Decimal
	err := r.pool.QueryRow(ctx,
		`SELECT available, held FROM wallet_balances WHERE wallet_id = $1 AND currency_id = $2`,
		walletID, currencyID).Scan(&avail, &held)
	if errors.Is(err, pgx.ErrNoRows) {
		return &domain.Balance{WalletID: walletID, CurrencyID: currencyID, Available: "0", Held: "0"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}
	return &domain.Balance{
		WalletID:   walletID,
		CurrencyID: currencyID,
		Available:  domain.FormatAmount(avail),
		Held:       domain.FormatAmount(held),
	}, nil
}

// ListEntries returns an order's append-only journal in posting order.
func (r *LedgerRepository) ListEntries(ctx context.Context, orderID string) ([]*domain.Entry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, order_id, op, wallet_id, bucket, currency_id, amount, created_at
		FROM entries WHERE order_id = $1 ORDER BY id`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	defer rows.Close()

	var out []*domain.Entry
	for rows.Next() {
		var (
			e      domain.Entry
			amount decimal.Decimal
		)
		if err := rows.Scan(&e.ID, &e.OrderID, &e.Op, &e.WalletID, &e.Bucket, &e.CurrencyID, &amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		e.Amount = domain.FormatAmount(amount)
		out = append(out, &e)
	}
	return out, rows.Err()
}
