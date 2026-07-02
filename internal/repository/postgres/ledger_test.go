package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/nvsces/ledger/internal/domain"
)

// testPool applies migrations and returns a pool, skipping if no DB is reachable.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://sc:sc@localhost:5432/ledger?sslmode=disable"
	}
	if err := Migrate(dsn); err != nil {
		t.Skipf("no DB / migrate failed: %v", err)
	}
	pool, err := Connect(context.Background(), dsn)
	if err != nil {
		t.Skipf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// reset removes any journal rows for the given orders and zeroes the given
// wallets, so a test starts from a clean slate regardless of prior runs (the
// tests use fixed order/wallet ids and share one database).
func reset(t *testing.T, pool *pgxpool.Pool, orders, wallets []string) {
	t.Helper()
	ctx := context.Background()
	if len(orders) > 0 {
		if _, err := pool.Exec(ctx, `DELETE FROM entries WHERE order_id = ANY($1)`, orders); err != nil {
			t.Fatalf("reset entries: %v", err)
		}
	}
	if len(wallets) > 0 {
		if _, err := pool.Exec(ctx, `DELETE FROM wallet_balances WHERE wallet_id = ANY($1)`, wallets); err != nil {
			t.Fatalf("reset balances: %v", err)
		}
	}
}

// seed sets a wallet's available balance directly, so tests start from funds.
func seed(t *testing.T, pool *pgxpool.Pool, walletID string, currencyID int64, available string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO wallet_balances (wallet_id, currency_id, available, held)
		VALUES ($1,$2,$3,0)
		ON CONFLICT (wallet_id, currency_id) DO UPDATE SET available = EXCLUDED.available, held = 0`,
		walletID, currencyID, available)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func bal(t *testing.T, r *LedgerRepository, wallet string, cur int64) (string, string) {
	t.Helper()
	b, err := r.GetBalance(context.Background(), wallet, cur)
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	return b.Available, b.Held
}

func dec(s string) decimal.Decimal { d, _ := decimal.NewFromString(s); return d }

func TestDepositIdempotentByRef(t *testing.T) {
	pool := testPool(t)
	r := NewLedgerRepository(pool)
	ctx := context.Background()

	wallet := "wlt_dep_idem"
	ref := "ton:111:deadbeef"
	reset(t, pool, []string{ref}, []string{wallet})

	applied, err := r.Deposit(ctx, ref, wallet, 1, dec("7.00"))
	if err != nil || !applied {
		t.Fatalf("first deposit: applied=%v err=%v", applied, err)
	}
	// Replayed deposit (same ref) is a no-op and must not double-credit.
	applied, err = r.Deposit(ctx, ref, wallet, 1, dec("7.00"))
	if err != nil {
		t.Fatalf("deposit replay err: %v", err)
	}
	if applied {
		t.Fatal("replayed deposit reported applied=true, want no-op")
	}
	if a, _ := bal(t, r, wallet, 1); a != "7" {
		t.Fatalf("after replay available=%s, want 7 (double-credited!)", a)
	}
}

// TestPostLegsConflictDoesNotDoubleCredit exercises the ON CONFLICT DO NOTHING
// guard directly: it applies the same (ref, op, wallet, bucket) leg twice through
// postLegs, bypassing the alreadyApplied short-circuit to simulate a concurrent
// writer that lost the race. The duplicate leg must insert nothing AND leave the
// balance untouched.
func TestPostLegsConflictDoesNotDoubleCredit(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	wallet := "wlt_postlegs_conflict"
	ref := "ton:222:cafebabe"
	reset(t, pool, []string{ref}, []string{wallet})

	legs := []leg{{wallet, domain.BucketAvailable, dec("3.50")}}

	apply := func() error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if err := postLegs(ctx, tx, ref, domain.OpDeposit, 1, legs); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	if err := apply(); err != nil {
		t.Fatalf("first postLegs: %v", err)
	}
	// Second application of the identical leg: the insert conflicts and is skipped,
	// so the balance stays put rather than being bumped a second time.
	if err := apply(); err != nil {
		t.Fatalf("duplicate postLegs returned error, want clean no-op: %v", err)
	}

	var available string
	if err := pool.QueryRow(ctx,
		`SELECT available::text FROM wallet_balances WHERE wallet_id=$1 AND currency_id=1`,
		wallet).Scan(&available); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	if available != "3.500000000000000000" {
		t.Fatalf("available=%s, want 3.5 (duplicate leg double-credited)", available)
	}

	// Exactly one journal row exists for the leg.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM entries WHERE order_id=$1 AND op=$2 AND wallet_id=$3 AND bucket=$4`,
		ref, string(domain.OpDeposit), wallet, string(domain.BucketAvailable)).Scan(&n); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if n != 1 {
		t.Fatalf("journal rows=%d, want 1", n)
	}
}

func TestFreezeCaptureHappyPath(t *testing.T) {
	pool := testPool(t)
	r := NewLedgerRepository(pool)
	ctx := context.Background()

	user := "wlt_user_cap"
	recv := "wlt_recv_cap"
	plat := "wlt_platform_cap"
	reset(t, pool, []string{"ord_cap"}, []string{user, recv, plat})
	seed(t, pool, user, 1, "10.00")

	// Freeze 5.00: available 10 -> 5, held 0 -> 5.
	applied, err := r.Freeze(ctx, "ord_cap", user, 1, dec("5.00"))
	if err != nil || !applied {
		t.Fatalf("Freeze: applied=%v err=%v", applied, err)
	}
	if a, h := bal(t, r, user, 1); a != "5" || h != "5" {
		t.Fatalf("after freeze user = (%s,%s), want (5,5)", a, h)
	}

	// Capture: net 4.75 to recv, fee 0.25 to platform; user held 5 -> 0.
	applied, err = r.Capture(ctx, "ord_cap", user, recv, plat, 1, dec("4.75"), dec("0.25"))
	if err != nil || !applied {
		t.Fatalf("Capture: applied=%v err=%v", applied, err)
	}
	if a, h := bal(t, r, user, 1); a != "5" || h != "0" {
		t.Fatalf("after capture user = (%s,%s), want (5,0)", a, h)
	}
	if a, _ := bal(t, r, recv, 1); a != "4.75" {
		t.Fatalf("recv available = %s, want 4.75", a)
	}
	if a, _ := bal(t, r, plat, 1); a != "0.25" {
		t.Fatalf("platform available = %s, want 0.25", a)
	}

	// Double-entry invariant: every entry set for the order sums to zero.
	assertJournalBalances(t, r, "ord_cap")
}

func TestFreezeReleaseReturnsFunds(t *testing.T) {
	pool := testPool(t)
	r := NewLedgerRepository(pool)
	ctx := context.Background()

	user := "wlt_user_rel"
	reset(t, pool, []string{"ord_rel"}, []string{user})
	seed(t, pool, user, 1, "8.00")

	if _, err := r.Freeze(ctx, "ord_rel", user, 1, dec("3.00")); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	applied, err := r.Release(ctx, "ord_rel", user, 1)
	if err != nil || !applied {
		t.Fatalf("Release: applied=%v err=%v", applied, err)
	}
	if a, h := bal(t, r, user, 1); a != "8" || h != "0" {
		t.Fatalf("after release user = (%s,%s), want (8,0)", a, h)
	}
	assertJournalBalances(t, r, "ord_rel")
}

func TestIdempotentReplay(t *testing.T) {
	pool := testPool(t)
	r := NewLedgerRepository(pool)
	ctx := context.Background()

	user := "wlt_user_idem"
	reset(t, pool, []string{"ord_idem"}, []string{user})
	seed(t, pool, user, 1, "10.00")

	if _, err := r.Freeze(ctx, "ord_idem", user, 1, dec("5.00")); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	// Replayed freeze must be a no-op (applied=false) and not double-debit.
	applied, err := r.Freeze(ctx, "ord_idem", user, 1, dec("5.00"))
	if err != nil {
		t.Fatalf("Freeze replay err: %v", err)
	}
	if applied {
		t.Fatal("replayed freeze reported applied=true, want no-op")
	}
	if a, h := bal(t, r, user, 1); a != "5" || h != "5" {
		t.Fatalf("after freeze replay = (%s,%s), want (5,5) — replay double-applied!", a, h)
	}
}

func TestInsufficientFundsRejected(t *testing.T) {
	pool := testPool(t)
	r := NewLedgerRepository(pool)
	ctx := context.Background()

	user := "wlt_user_broke"
	reset(t, pool, []string{"ord_broke"}, []string{user})
	seed(t, pool, user, 1, "2.00")

	_, err := r.Freeze(ctx, "ord_broke", user, 1, dec("5.00"))
	if err == nil {
		t.Fatal("expected insufficient-funds error")
	}
	if !errors.Is(err, domain.ErrInsufficient) {
		t.Fatalf("err = %v, want ErrInsufficient", err)
	}
	// Balance untouched and nothing journaled.
	if a, h := bal(t, r, user, 1); a != "2" || h != "0" {
		t.Fatalf("after rejected freeze = (%s,%s), want (2,0)", a, h)
	}
	entries, _ := r.ListEntries(ctx, "ord_broke")
	if len(entries) != 0 {
		t.Fatalf("rejected freeze left %d journal rows, want 0", len(entries))
	}
}

func TestCreateAccountIdempotentAndDepositByMemo(t *testing.T) {
	pool := testPool(t)
	r := NewLedgerRepository(pool)
	ctx := context.Background()

	user := "u_acct_test"
	_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE user_id = $1`, user)

	acc := &domain.Account{
		WalletID: "wlt_acct_1", UserID: user, TONAddress: "UQ_shared", Memo: "memo-abc", CurrencyID: 1,
	}
	got, err := r.CreateAccount(ctx, acc)
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if got.WalletID != "wlt_acct_1" || got.Memo != "memo-abc" {
		t.Fatalf("account = %+v", got)
	}

	// Idempotent: a second create for same (user,currency) with a DIFFERENT wallet
	// returns the original account, not a duplicate.
	dup := &domain.Account{WalletID: "wlt_acct_2", UserID: user, TONAddress: "UQ_shared", Memo: "memo-xyz", CurrencyID: 1}
	got2, err := r.CreateAccount(ctx, dup)
	if err != nil {
		t.Fatalf("CreateAccount dup: %v", err)
	}
	if got2.WalletID != "wlt_acct_1" {
		t.Fatalf("idempotency broken: got wallet %s, want wlt_acct_1", got2.WalletID)
	}

	// Resolve by memo, then credit a deposit and check the balance landed.
	byMemo, err := r.AccountByMemo(ctx, "memo-abc")
	if err != nil || byMemo.WalletID != "wlt_acct_1" {
		t.Fatalf("AccountByMemo: %+v err=%v", byMemo, err)
	}
	reset(t, pool, []string{"dep_ref_1"}, []string{"wlt_acct_1"})
	applied, err := r.Deposit(ctx, "dep_ref_1", byMemo.WalletID, 1, dec("12.50"))
	if err != nil || !applied {
		t.Fatalf("Deposit: applied=%v err=%v", applied, err)
	}
	if a, h := bal(t, r, "wlt_acct_1", 1); a != "12.5" || h != "0" {
		t.Fatalf("after deposit = (%s,%s), want (12.5,0)", a, h)
	}
	// Replayed deposit (same ref) is a no-op.
	applied, err = r.Deposit(ctx, "dep_ref_1", byMemo.WalletID, 1, dec("12.50"))
	if err != nil || applied {
		t.Fatalf("Deposit replay: applied=%v err=%v (want no-op)", applied, err)
	}
	if a, _ := bal(t, r, "wlt_acct_1", 1); a != "12.5" {
		t.Fatalf("deposit replay double-credited: available=%s", a)
	}
}

func TestAccountsByUser(t *testing.T) {
	pool := testPool(t)
	r := NewLedgerRepository(pool)
	ctx := context.Background()

	user := "u_list_accounts"
	_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE user_id = $1`, user)

	// One account per seeded currency (1=DEV, 2=GRAM).
	for _, c := range []struct {
		wallet string
		memo   string
		ccy    int64
	}{
		{"wlt_list_1", "memo-list-1", 1},
		{"wlt_list_2", "memo-list-2", 2},
	} {
		if _, err := r.CreateAccount(ctx, &domain.Account{
			WalletID: c.wallet, UserID: user, TONAddress: "UQ_shared", Memo: c.memo, CurrencyID: c.ccy,
		}); err != nil {
			t.Fatalf("CreateAccount %d: %v", c.ccy, err)
		}
	}

	accs, err := r.AccountsByUser(ctx, user)
	if err != nil {
		t.Fatalf("AccountsByUser: %v", err)
	}
	if len(accs) != 2 {
		t.Fatalf("got %d accounts, want 2", len(accs))
	}
	// Ordered by currency_id.
	if accs[0].CurrencyID != 1 || accs[1].CurrencyID != 2 {
		t.Fatalf("order = [%d,%d], want [1,2]", accs[0].CurrencyID, accs[1].CurrencyID)
	}

	// An unknown user gets an empty list, not an error.
	empty, err := r.AccountsByUser(ctx, "u_no_such_user")
	if err != nil {
		t.Fatalf("AccountsByUser(unknown): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("unknown user returned %d accounts, want 0", len(empty))
	}
}

func TestPlatformWalletSeededPerCurrency(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// The seed migration (0005) must create a platform balance row for every
	// currency in the catalog, so the per-currency fee account exists before any
	// capture — including currencies (e.g. GRAM) that have never been paid into.
	var missing int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM currencies c
		WHERE NOT EXISTS (
			SELECT 1 FROM wallet_balances b
			WHERE b.wallet_id = 'wlt_platform' AND b.currency_id = c.id
		)`).Scan(&missing)
	if err != nil {
		t.Fatalf("query missing platform balances: %v", err)
	}
	if missing != 0 {
		t.Fatalf("%d currencies have no platform balance row, want 0", missing)
	}

	// Both known currencies (1=DEV, 2=GRAM) must be present.
	for _, ccy := range []int64{1, 2} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM wallet_balances WHERE wallet_id = 'wlt_platform' AND currency_id = $1`,
			ccy).Scan(&n); err != nil {
			t.Fatalf("query platform ccy %d: %v", ccy, err)
		}
		if n != 1 {
			t.Fatalf("platform balance rows for ccy %d = %d, want 1", ccy, n)
		}
	}
}

// assertJournalBalances checks the double-entry invariant: the signed entry
// amounts for an order sum to exactly zero.
func assertJournalBalances(t *testing.T, r *LedgerRepository, orderID string) {
	t.Helper()
	entries, err := r.ListEntries(context.Background(), orderID)
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no journal entries for %s", orderID)
	}
	sum := decimal.Zero
	for _, e := range entries {
		sum = sum.Add(dec(e.Amount))
	}
	if !sum.IsZero() {
		t.Fatalf("journal for %s sums to %s, want 0 (double-entry broken)", orderID, sum)
	}
}
