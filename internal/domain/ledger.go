package domain

import (
	"errors"
	"time"
)

// Bucket is the portion of a wallet an entry posts against. A wallet's spendable
// money lives in AVAILABLE; funds reserved for an in-flight order sit in HELD.
type Bucket string

const (
	BucketAvailable Bucket = "AVAILABLE"
	BucketHeld      Bucket = "HELD"
)

// Op is the ledger operation that produced a set of entries. It is recorded on
// each journal row and, together with OrderID, forms the idempotency key.
type Op string

const (
	OpFreeze  Op = "FREEZE"
	OpCapture Op = "CAPTURE"
	OpRelease Op = "RELEASE"
	// OpDeposit credits external funds (e.g. an on-chain TON transfer) into a
	// wallet's AVAILABLE bucket. It is the one operation whose entries do not sum
	// to zero — money enters the system from outside.
	OpDeposit Op = "DEPOSIT"
)

// Entry is one leg of a double-entry posting: a signed amount against a
// (wallet, bucket, currency) account. Within a single operation the signed
// amounts sum to exactly zero — money is only ever moved, never created or
// destroyed. Rows are append-only; balances are the running sum of entries.
type Entry struct {
	ID         int64
	OrderID    string
	Op         Op
	WalletID   string
	Bucket     Bucket
	CurrencyID int64
	// Amount is the signed magnitude as a decimal string: negative debits a
	// bucket, positive credits it.
	Amount    string
	CreatedAt time.Time
}

// Balance is a wallet's derived position for one currency.
type Balance struct {
	WalletID   string
	CurrencyID int64
	Available  string
	Held       string
}

// Account is a client's ledger account: the wallet holding their balance plus
// the on-chain deposit routing (a shared TON address + a unique memo tag in the
// demo). The ledger owns this so deposits can be matched to a wallet by memo.
type Account struct {
	WalletID   string
	UserID     string
	TONAddress string
	Memo       string
	CurrencyID int64
	CreatedAt  time.Time
}

// Account-related sentinel errors.
var (
	ErrAccountNotFound = errors.New("account not found")
)
