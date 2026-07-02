// Package service holds the ledger's application logic: it validates inputs,
// parses money, and delegates the atomic double-entry postings to the store.
package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/nvsces/ledger/internal/domain"
)

// Store is the persistence port the service depends on. The Postgres
// implementation applies each operation as one atomic, idempotent transaction.
type Store interface {
	CreateAccount(ctx context.Context, a *domain.Account) (*domain.Account, error)
	AccountByMemo(ctx context.Context, memo string) (*domain.Account, error)
	Freeze(ctx context.Context, orderID, walletID string, currencyID int64, amount decimal.Decimal) (bool, error)
	Capture(ctx context.Context, orderID, walletID, receivingWalletID, platformWalletID string, currencyID int64, net, fee decimal.Decimal) (bool, error)
	Release(ctx context.Context, orderID, walletID string, currencyID int64) (bool, error)
	Deposit(ctx context.Context, ref, walletID string, currencyID int64, amount decimal.Decimal) (bool, error)
	GetBalance(ctx context.Context, walletID string, currencyID int64) (*domain.Balance, error)
	ListEntries(ctx context.Context, orderID string) ([]*domain.Entry, error)
	ListCurrencies(ctx context.Context) ([]*domain.Currency, error)
	GetCurrency(ctx context.Context, id int64) (*domain.Currency, error)
}

// Ledger is the application service. platformWallet is the destination for
// captured fees; tonAddress is the shared on-chain deposit address handed to
// every account (the demo distinguishes users by memo, not address).
type Ledger struct {
	store          Store
	platformWallet string
	tonAddress     string
	now            func() time.Time
}

// New builds a Ledger over a store. platformWallet is where capture fees land;
// tonAddress is the shared deposit address stamped on every new account.
func New(store Store, platformWallet, tonAddress string, opts ...Option) *Ledger {
	l := &Ledger{
		store:          store,
		platformWallet: platformWallet,
		tonAddress:     tonAddress,
		now:            func() time.Time { return time.Now().UTC() },
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

// Option configures a Ledger.
type Option func(*Ledger)

// WithClock overrides the time source (tests).
func WithClock(c func() time.Time) Option { return func(l *Ledger) { l.now = c } }

// CreateAccount provisions (or returns) the account for a user+currency. It
// generates a wallet_id and a unique deposit memo, stamping the shared TON
// address. Idempotent per (user, currency).
func (l *Ledger) CreateAccount(ctx context.Context, userID string, currencyID int64) (*domain.Account, error) {
	if userID == "" {
		return nil, domain.ErrInvalidAmount
	}
	// Reject unknown currencies up front (the accounts FK would also catch this,
	// but a clean domain error is clearer than a constraint violation).
	if _, err := l.store.GetCurrency(ctx, currencyID); err != nil {
		return nil, err
	}
	acc := &domain.Account{
		WalletID:   "wlt_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		UserID:     userID,
		TONAddress: l.tonAddress,
		Memo:       newMemo(),
		CurrencyID: currencyID,
		CreatedAt:  l.now(),
	}
	return l.store.CreateAccount(ctx, acc)
}

// AccountByMemo resolves the account a deposit belongs to by its memo tag.
func (l *Ledger) AccountByMemo(ctx context.Context, memo string) (*domain.Account, error) {
	if memo == "" {
		return nil, domain.ErrAccountNotFound
	}
	return l.store.AccountByMemo(ctx, memo)
}

// ListCurrencies returns all known currencies (the reference catalog).
func (l *Ledger) ListCurrencies(ctx context.Context) ([]*domain.Currency, error) {
	return l.store.ListCurrencies(ctx)
}

// newMemo returns a short, unique deposit tag. TON memos are free-form text; a
// hex slice of a UUID is compact and collision-resistant for the demo.
func newMemo() string {
	return "memo-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
}

// Freeze validates and reserves funds. Returns applied=false on idempotent replay.
func (l *Ledger) Freeze(ctx context.Context, orderID, walletID string, currencyID int64, amount string) (bool, error) {
	if orderID == "" || walletID == "" {
		return false, domain.ErrInvalidAmount
	}
	amt, err := domain.ParseAmount(amount)
	if err != nil {
		return false, err
	}
	return l.store.Freeze(ctx, orderID, walletID, currencyID, amt)
}

// Capture settles held funds: net to the receiving wallet, fee to the platform.
func (l *Ledger) Capture(ctx context.Context, orderID, walletID, receivingWalletID string, currencyID int64, net, fee string) (bool, error) {
	if orderID == "" || walletID == "" || receivingWalletID == "" {
		return false, domain.ErrInvalidAmount
	}
	netD, err := decimal.NewFromString(net)
	if err != nil || netD.IsNegative() {
		return false, domain.ErrInvalidAmount
	}
	feeD, err := decimal.NewFromString(fee)
	if err != nil || feeD.IsNegative() {
		return false, domain.ErrInvalidAmount
	}
	if netD.Add(feeD).IsZero() {
		return false, domain.ErrNonPositive
	}
	return l.store.Capture(ctx, orderID, walletID, receivingWalletID, l.platformWallet, currencyID, netD, feeD)
}

// Release returns an order's held funds to the wallet's available balance.
func (l *Ledger) Release(ctx context.Context, orderID, walletID string, currencyID int64) (bool, error) {
	if orderID == "" || walletID == "" {
		return false, domain.ErrInvalidAmount
	}
	return l.store.Release(ctx, orderID, walletID, currencyID)
}

// Deposit credits external funds (e.g. an on-chain TON transfer) into a wallet.
// ref is the idempotency key.
func (l *Ledger) Deposit(ctx context.Context, ref, walletID string, currencyID int64, amount string) (bool, error) {
	if ref == "" || walletID == "" {
		return false, domain.ErrInvalidAmount
	}
	amt, err := domain.ParseAmount(amount)
	if err != nil {
		return false, err
	}
	return l.store.Deposit(ctx, ref, walletID, currencyID, amt)
}

// Balance returns a wallet's available/held for a currency.
func (l *Ledger) Balance(ctx context.Context, walletID string, currencyID int64) (*domain.Balance, error) {
	return l.store.GetBalance(ctx, walletID, currencyID)
}

// Entries returns an order's append-only journal.
func (l *Ledger) Entries(ctx context.Context, orderID string) ([]*domain.Entry, error) {
	return l.store.ListEntries(ctx, orderID)
}
