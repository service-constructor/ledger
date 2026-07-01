// Package domain holds the ledger's core types: accounts, buckets, and the
// double-entry journal. Money is kept as decimal strings on the wire and parsed
// into shopspring/decimal for exact arithmetic — never floats.
package domain

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// Ledger sentinel errors.
var (
	ErrInvalidAmount    = errors.New("invalid amount")
	ErrNonPositive      = errors.New("amount must be positive")
	ErrInsufficient     = errors.New("insufficient funds")
	ErrWalletNotFound   = errors.New("wallet not found")
	ErrCurrencyMismatch = errors.New("currency mismatch")
)

// ParseAmount parses a decimal string and requires it to be strictly positive.
// Freeze/Capture/Release amounts are always positive magnitudes; the signed
// journal entries derive their sign from the posting direction, not the input.
func ParseAmount(s string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, fmt.Errorf("%w: %q", ErrInvalidAmount, s)
	}
	if !d.IsPositive() {
		return decimal.Zero, fmt.Errorf("%w: %s", ErrNonPositive, s)
	}
	return d, nil
}

// FormatAmount renders a decimal back to a canonical string for the wire.
func FormatAmount(d decimal.Decimal) string {
	return d.String()
}
