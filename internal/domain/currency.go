package domain

import "errors"

// Currency IDs are stable, well-known constants. id 1 is the dev/test currency
// that predates the currencies table; id 2 is Gram (the TON unit), real money.
const (
	CurrencyDev     int64 = 1
	CurrencyGramTON int64 = 2
)

// Currency is a first-class reference entity: every wallet/entry currency_id
// resolves to one of these. is_real distinguishes test money (mock-fundable)
// from real money (funded only by on-chain deposits).
type Currency struct {
	ID       int64
	Code     string // short machine code, e.g. DEV, GRAM
	Name     string // display name
	Symbol   string // display symbol/ticker
	Decimals int32  // on-chain precision (informational)
	IsReal   bool   // false = test/dev money, true = backed by a real chain
}

// ErrCurrencyNotFound is returned when an operation references an unknown
// currency_id (one with no row in the currencies table).
var ErrCurrencyNotFound = errors.New("currency not found")
