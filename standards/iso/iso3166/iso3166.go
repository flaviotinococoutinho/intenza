//intenza:paradigm data-oriented

// Package iso3166 is the ISO 3166-1 list of countries and territories with
// alpha-2, alpha-3 and numeric codes, chained to the UN M49 areas above them.
//
// Three keys, three rank dictionaries: alpha-2 over 676 slots (88 bytes of
// bits), alpha-3 over 17 576 and numeric over 1 000, the last two with a
// permutation back to record order. Every lookup is O(1) and allocation-free.
package iso3166

//go:generate go run ../../internal/gen iso3166

import (
	"errors"
	"iter"

	"github.com/flaviotinococoutinho/intenza/standards/internal/fold"
	"github.com/flaviotinococoutinho/intenza/standards/internal/succinct"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
	"github.com/flaviotinococoutinho/intenza/standards/un/m49"
)

// Country is a handle to an ISO 3166-1 entry. The zero value is invalid.
type Country uint16

// ErrUnknown is returned for codes outside the list.
var ErrUnknown = errors.New("iso3166: unknown country")

// ByAlpha2 finds a country by its two-letter code, in either case.
func ByAlpha2(code string) (Country, bool) {
	k, ok := succinct.Alpha2(code)
	if !ok {
		return 0, false
	}
	i, ok := alpha2Rank.Index(k)
	return Country(i + 1), ok
}

// ByAlpha3 finds a country by its three-letter code.
func ByAlpha3(code string) (Country, bool) {
	k, ok := succinct.Alpha3(code)
	if !ok {
		return 0, false
	}
	i, ok := alpha3Rank.Index(k)
	if !ok {
		return 0, false
	}
	return Country(alpha3Order[i] + 1), true
}

// ByNumeric finds a country by its numeric code (76 for Brazil).
func ByNumeric(n int) (Country, bool) {
	if n < 0 || n > 999 {
		return 0, false
	}
	i, ok := numRank.Index(uint32(n))
	if !ok {
		return 0, false
	}
	return Country(numOrder[i] + 1), true
}

// Parse accepts alpha-2, alpha-3 or a three-digit numeric code.
func Parse(s string) (Country, error) {
	switch len(s) {
	case 2:
		if c, ok := ByAlpha2(s); ok {
			return c, nil
		}
	case 3:
		if c, ok := ByAlpha3(s); ok {
			return c, nil
		}
		if n, ok := succinct.Digits(s); ok {
			if c, ok := ByNumeric(int(n)); ok {
				return c, nil
			}
		}
	}
	return 0, ErrUnknown
}

// Valid reports whether c points at an entry.
func (c Country) Valid() bool { return c > 0 && int(c) <= count }

func (c Country) i() int { return int(c) - 1 }

// Alpha2 is the two-letter code.
func (c Country) Alpha2() string {
	if !c.Valid() {
		return ""
	}
	return alpha2Codes[2*c.i() : 2*c.i()+2]
}

// String is the alpha-2 code.
func (c Country) String() string { return c.Alpha2() }

// Alpha3 is the three-letter code.
func (c Country) Alpha3() string {
	if !c.Valid() {
		return ""
	}
	return alpha3Codes[3*c.i() : 3*c.i()+3]
}

// Numeric is the numeric code.
func (c Country) Numeric() int {
	if !c.Valid() {
		return 0
	}
	return int(numeric[c.i()])
}

// NumericString is the zero-padded numeric code ("076").
func (c Country) NumericString() string {
	if !c.Valid() {
		return ""
	}
	return numericCodes[3*c.i() : 3*c.i()+3]
}

// Name is the English short name.
func (c Country) Name() string {
	if !c.Valid() {
		return ""
	}
	return nameEN[nameENOff[c.i()]:nameENOff[c.i()+1]]
}

// NamePT is the Brazilian Portuguese name.
func (c Country) NamePT() string {
	if !c.Valid() {
		return ""
	}
	return namePT[namePTOff[c.i()]:namePTOff[c.i()+1]]
}

// Area is the most specific M49 area the country belongs to.
func (c Country) Area() m49.Area {
	if !c.Valid() {
		return 0
	}
	a, _ := m49.ByCode(int(area[c.i()]))
	return a
}

// In reports whether the country lies inside the given M49 area.
func (c Country) In(a m49.Area) bool { return a.Contains(c.Area()) }

// MarshalText encodes the alpha-2 code.
func (c Country) MarshalText() ([]byte, error) {
	if !c.Valid() {
		return nil, ErrUnknown
	}
	return []byte(c.Alpha2()), nil
}

// UnmarshalText decodes any of the three codes.
func (c *Country) UnmarshalText(b []byte) error {
	v, err := Parse(string(b))
	if err != nil {
		return err
	}
	*c = v
	return nil
}

// All yields every country in alpha-2 order.
func All() iter.Seq[Country] {
	return func(yield func(Country) bool) {
		for i := 1; i <= count; i++ {
			if !yield(Country(i)) {
				return
			}
		}
	}
}

// Search appends the countries whose codes or names have every word of q as
// a prefix ("estados uni" → US). Case and accent insensitive.
func Search(dst []Country, q string) []Country {
	var buf [64]byte
	set, ok := tokens.Match(fold.Append(buf[:0], q))
	if !ok {
		return dst
	}
	for i := range set.All() {
		dst = append(dst, Country(i+1))
	}
	return dst
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "iso-3166-1",
		Code:      "ISO 3166-1",
		Title:     "Codes for the representation of names of countries and their subdivisions — Part 1: Country codes",
		Authority: meta.ISO,
		Kind:      meta.CodeList,
		Edition:   "ISO 3166-1:2020 com boletins da agência de manutenção, via datasets/country-codes",
		Package:   "iso/iso3166",
		Structure: "três dicionários de rank (alpha-2, alpha-3, numérico) com permutação para a ordem dos registros; árvore M49 acima",
		Verification: "ida e volta exaustiva entre as três chaves para os 249 registros; toda área M49 referida existe; " +
			"amostras conferidas contra a plataforma da ISO",
		Sources: []meta.Source{
			{
				Name: "datasets/country-codes (compilação ISO/UNSD)", URL: "https://github.com/datasets/country-codes",
				Retrieved: "2026-10-14", SHA256: sourceSHA256, License: "ODC-PDDL", Grounding: meta.Secondary,
			},
			{
				Name: "Unicode CLDR pt.xml (nomes em português)", URL: "https://github.com/unicode-org/cldr",
				Retrieved: "2026-10-14", License: "Unicode-3.0", Grounding: meta.Secondary,
			},
		},
	}
}
