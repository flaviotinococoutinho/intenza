//intenza:paradigm data-oriented

// Package iso4217 is the ISO 4217 currency code list: alphabetic code, numeric
// code and minor units, as published by the maintenance agency.
//
// A Currency is a 2-byte handle into pointer-free tables generated from the
// committed extract. Lookups by alphabetic or numeric code are O(1) rank
// queries; accessors slice constant strings and never allocate.
package iso4217

//go:generate go run ../../internal/gen iso4217

import (
	"errors"
	"iter"

	"github.com/flaviotinococoutinho/intenza/standards/internal/fold"
	"github.com/flaviotinococoutinho/intenza/standards/internal/succinct"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

// Currency is a handle to an ISO 4217 entry. The zero value is invalid.
type Currency uint16

// ErrUnknown is returned for codes outside the list.
var ErrUnknown = errors.New("iso4217: unknown currency")

// ByAlpha finds a currency by its alphabetic code, in either case.
func ByAlpha(code string) (Currency, bool) {
	k, ok := succinct.Alpha3(code)
	if !ok {
		return 0, false
	}
	i, ok := alphaRank.Index(k)
	return Currency(i + 1), ok
}

// ByNumeric finds a currency by its numeric code.
func ByNumeric(n int) (Currency, bool) {
	if n < 0 || n > 999 {
		return 0, false
	}
	i, ok := numRank.Index(uint32(n))
	if !ok {
		return 0, false
	}
	return Currency(numOrder[i] + 1), true
}

// Parse accepts an alphabetic ("BRL") or numeric ("986") code.
func Parse(s string) (Currency, error) {
	if c, ok := ByAlpha(s); ok {
		return c, nil
	}
	if n, ok := succinct.Digits(s); ok && len(s) == 3 {
		if c, ok := ByNumeric(int(n)); ok {
			return c, nil
		}
	}
	return 0, ErrUnknown
}

// Valid reports whether c points at an entry.
func (c Currency) Valid() bool { return c > 0 && int(c) <= count }

func (c Currency) i() int { return int(c) - 1 }

// Alpha is the three-letter code.
func (c Currency) Alpha() string {
	if !c.Valid() {
		return ""
	}
	return alphaCodes[3*c.i() : 3*c.i()+3]
}

// String is the alphabetic code.
func (c Currency) String() string { return c.Alpha() }

// Numeric is the numeric code (986 for BRL).
func (c Currency) Numeric() int {
	if !c.Valid() {
		return 0
	}
	return int(numeric[c.i()])
}

// NumericString is the zero-padded numeric code ("986"), as payment
// messages such as the Pix BR Code carry it.
func (c Currency) NumericString() string {
	if !c.Valid() {
		return ""
	}
	return numericCodes[3*c.i() : 3*c.i()+3]
}

// MinorUnits is the number of decimal places; ok is false where the
// standard says N.A. (precious metals, testing codes).
func (c Currency) MinorUnits() (digits int, ok bool) {
	if !c.Valid() || minorUnits[c.i()] < 0 {
		return 0, false
	}
	return int(minorUnits[c.i()]), true
}

// Fund reports whether the code is a fund code (an accounting unit, not cash).
func (c Currency) Fund() bool { return c.Valid() && fund[c.i()] == 1 }

// Entities is how many countries or territories use the currency.
func (c Currency) Entities() int {
	if !c.Valid() {
		return 0
	}
	return int(entities[c.i()])
}

// Name is the English name from the standard.
func (c Currency) Name() string {
	if !c.Valid() {
		return ""
	}
	return nameEN[nameENOff[c.i()]:nameENOff[c.i()+1]]
}

// NamePT is the Brazilian Portuguese display name.
func (c Currency) NamePT() string {
	if !c.Valid() {
		return ""
	}
	return namePT[namePTOff[c.i()]:namePTOff[c.i()+1]]
}

// MarshalText encodes the alphabetic code.
func (c Currency) MarshalText() ([]byte, error) {
	if !c.Valid() {
		return nil, ErrUnknown
	}
	return []byte(c.Alpha()), nil
}

// UnmarshalText decodes an alphabetic or numeric code.
func (c *Currency) UnmarshalText(b []byte) error {
	v, err := Parse(string(b))
	if err != nil {
		return err
	}
	*c = v
	return nil
}

// All yields every currency in alphabetic order.
func All() iter.Seq[Currency] {
	return func(yield func(Currency) bool) {
		for i := 1; i <= count; i++ {
			if !yield(Currency(i)) {
				return
			}
		}
	}
}

// Search appends to dst the currencies whose code or names have every word of
// q as a prefix ("real bra" → BRL). It is case and accent insensitive.
func Search(dst []Currency, q string) []Currency {
	var buf [64]byte
	set, ok := tokens.Match(fold.Append(buf[:0], q))
	if !ok {
		return dst
	}
	for i := range set.All() {
		dst = append(dst, Currency(i+1))
	}
	return dst
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "iso-4217",
		Code:      "ISO 4217",
		Title:     "Codes for the representation of currencies",
		Authority: meta.ISO,
		Kind:      meta.CodeList,
		Edition:   "ISO 4217:2015, lista da agência de manutenção publicada em 2026-09-17",
		Package:   "iso/iso4217",
		Structure: "handle de 2 bytes; rank sobre 26³ chaves (2,7 KB) para o alfa e sobre 1000 para o numérico; strings fatiadas de constantes",
		Verification: "contagem e amostras conferidas contra a lista da SIX; testes exaustivos de ida e volta alfa ↔ numérico; " +
			"digest do extrato embutido no código gerado",
		Sources: []meta.Source{
			{
				Name: "SIX Financial Information — list one", URL: "https://www.six-group.com/en/products-services/financial-information/data-standards.html",
				Retrieved: "2026-10-14", SHA256: sourceSHA256, License: "dados públicos da agência de manutenção", Grounding: meta.Primary,
			},
			{
				Name: "Unicode CLDR pt.xml (nomes em português)", URL: "https://github.com/unicode-org/cldr",
				Retrieved: "2026-10-14", License: "Unicode-3.0", Grounding: meta.Secondary,
			},
		},
	}
}
