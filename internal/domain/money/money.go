//intenza:paradigm functional-core

// Package money is an amount in the minor units of an ISO 4217 currency.
// Integer arithmetic only: no float ever touches a price.
package money

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/flaviotinococoutinho/intenza/standards/iso/iso4217"
)

var (
	ErrCurrency = errors.New("money: currency mismatch or without minor units")
	ErrOverflow = errors.New("money: overflow")
	ErrFormat   = errors.New("money: malformed amount")
)

// Money is an amount in minor units (centavos for BRL).
type Money struct {
	Minor    int64
	Currency iso4217.Currency
}

// New checks that the currency has a defined number of minor units.
func New(minor int64, c iso4217.Currency) (Money, error) {
	if _, ok := c.MinorUnits(); !ok {
		return Money{}, ErrCurrency
	}
	return Money{Minor: minor, Currency: c}, nil
}

// BRL is a shortcut for reais.
func BRL(centavos int64) Money { return Money{Minor: centavos, Currency: iso4217.BRL} }

// Parse reads a decimal amount ("1234.56") in the currency.
func Parse(s string, c iso4217.Currency) (Money, error) {
	digits, ok := c.MinorUnits()
	if !ok {
		return Money{}, ErrCurrency
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || len(frac) > digits {
		return Money{}, ErrFormat
	}
	frac += strings.Repeat("0", digits-len(frac))
	n, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return Money{}, ErrFormat
	}
	if neg {
		n = -n
	}
	return Money{Minor: n, Currency: c}, nil
}

// Add sums two amounts of the same currency.
func (m Money) Add(o Money) (Money, error) {
	if m.Currency != o.Currency {
		return Money{}, ErrCurrency
	}
	if (o.Minor > 0 && m.Minor > math.MaxInt64-o.Minor) || (o.Minor < 0 && m.Minor < math.MinInt64-o.Minor) {
		return Money{}, ErrOverflow
	}
	return Money{Minor: m.Minor + o.Minor, Currency: m.Currency}, nil
}

// Positive reports whether the amount is above zero.
func (m Money) Positive() bool { return m.Minor > 0 }

// Decimal renders the amount with the currency's decimals ("1234.56").
func (m Money) Decimal() string {
	digits, _ := m.Currency.MinorUnits()
	n := m.Minor
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	s := strconv.FormatInt(n, 10)
	for len(s) <= digits {
		s = "0" + s
	}
	if digits == 0 {
		return sign + s
	}
	return sign + s[:len(s)-digits] + "." + s[len(s)-digits:]
}

func (m Money) String() string { return fmt.Sprintf("%s %s", m.Currency.Alpha(), m.Decimal()) }

type wire struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// MarshalJSON encodes {"amount": "1234.56", "currency": "BRL"}.
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(wire{Amount: m.Decimal(), Currency: m.Currency.Alpha()})
}

// UnmarshalJSON decodes the same shape.
func (m *Money) UnmarshalJSON(b []byte) error {
	var w wire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	c, err := iso4217.Parse(w.Currency)
	if err != nil {
		return err
	}
	v, err := Parse(w.Amount, c)
	if err != nil {
		return err
	}
	*m = v
	return nil
}
