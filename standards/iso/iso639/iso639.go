//intenza:paradigm data-oriented

// Package iso639 is the ISO 639-2 language code list with the ISO 639-1
// alpha-2 forms where they exist and both alpha-3 variants (bibliographic and
// terminological, which differ for twenty languages such as ger/deu).
package iso639

//go:generate go run ../../internal/gen iso639

import (
	"errors"
	"iter"

	"github.com/flaviotinococoutinho/intenza/standards/internal/fold"
	"github.com/flaviotinococoutinho/intenza/standards/internal/succinct"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

// Language is a handle to an ISO 639-2 entry. The zero value is invalid.
type Language uint16

// ErrUnknown is returned for codes outside the list.
var ErrUnknown = errors.New("iso639: unknown language")

// ByAlpha2 finds a language by its ISO 639-1 code ("pt").
func ByAlpha2(code string) (Language, bool) {
	k, ok := succinct.Alpha2(code)
	if !ok {
		return 0, false
	}
	i, ok := a2Rank.Index(k)
	if !ok {
		return 0, false
	}
	return Language(a2Order[i] + 1), true
}

// ByAlpha3 finds a language by either alpha-3 code ("por", "ger" or "deu").
func ByAlpha3(code string) (Language, bool) {
	k, ok := succinct.Alpha3(code)
	if !ok {
		return 0, false
	}
	if i, ok := b3Rank.Index(k); ok {
		return Language(i + 1), true
	}
	i, ok := t3Rank.Index(k)
	if !ok {
		return 0, false
	}
	return Language(t3Order[i] + 1), true
}

// Parse accepts an alpha-2 or alpha-3 code.
func Parse(s string) (Language, error) {
	var (
		l  Language
		ok bool
	)
	switch len(s) {
	case 2:
		l, ok = ByAlpha2(s)
	case 3:
		l, ok = ByAlpha3(s)
	}
	if !ok {
		return 0, ErrUnknown
	}
	return l, nil
}

// Valid reports whether l points at an entry.
func (l Language) Valid() bool { return l > 0 && int(l) <= count }

func (l Language) i() int { return int(l) - 1 }

// Alpha2 is the ISO 639-1 code; ok is false for languages without one.
func (l Language) Alpha2() (string, bool) {
	if !l.Valid() || alpha2Codes[2*l.i()] == ' ' {
		return "", false
	}
	return alpha2Codes[2*l.i() : 2*l.i()+2], true
}

// Alpha3B is the bibliographic alpha-3 code.
func (l Language) Alpha3B() string {
	if !l.Valid() {
		return ""
	}
	return alpha3B[3*l.i() : 3*l.i()+3]
}

// Alpha3T is the terminological alpha-3 code (equal to B for most languages).
func (l Language) Alpha3T() string {
	if !l.Valid() {
		return ""
	}
	return alpha3T[3*l.i() : 3*l.i()+3]
}

// String is the alpha-2 code when there is one, else the terminological code.
func (l Language) String() string {
	if a2, ok := l.Alpha2(); ok {
		return a2
	}
	return l.Alpha3T()
}

// Name is the English name as registered.
func (l Language) Name() string {
	if !l.Valid() {
		return ""
	}
	return nameEN[nameENOff[l.i()]:nameENOff[l.i()+1]]
}

// NamePT is the Brazilian Portuguese name.
func (l Language) NamePT() string {
	if !l.Valid() {
		return ""
	}
	return namePT[namePTOff[l.i()]:namePTOff[l.i()+1]]
}

// MarshalText encodes String().
func (l Language) MarshalText() ([]byte, error) {
	if !l.Valid() {
		return nil, ErrUnknown
	}
	return []byte(l.String()), nil
}

// UnmarshalText decodes an alpha-2 or alpha-3 code.
func (l *Language) UnmarshalText(b []byte) error {
	v, err := Parse(string(b))
	if err != nil {
		return err
	}
	*l = v
	return nil
}

// All yields every language in bibliographic-code order.
func All() iter.Seq[Language] {
	return func(yield func(Language) bool) {
		for i := 1; i <= count; i++ {
			if !yield(Language(i)) {
				return
			}
		}
	}
}

// Search appends the languages whose codes or names have every word of q as a
// prefix ("portug" → pt). Case and accent insensitive.
func Search(dst []Language, q string) []Language {
	var buf [64]byte
	set, ok := tokens.Match(fold.Append(buf[:0], q))
	if !ok {
		return dst
	}
	for i := range set.All() {
		dst = append(dst, Language(i+1))
	}
	return dst
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:           "iso-639",
		Code:         "ISO 639-1/639-2",
		Title:        "Codes for the representation of names of languages",
		Authority:    meta.ISO,
		Kind:         meta.CodeList,
		Edition:      "registro da Library of Congress (RA da ISO 639-2), via datasets/language-codes",
		Package:      "iso/iso639",
		Structure:    "rank sobre 26³ para o alfa-3 bibliográfico, rank + permutação para o terminológico e para o alfa-2",
		Verification: "ida e volta exaustiva alfa-2 ↔ alfa-3; os vinte pares B/T divergentes resolvem para o mesmo idioma",
		Sources: []meta.Source{
			{
				Name: "datasets/language-codes (compilação da Library of Congress)", URL: "https://github.com/datasets/language-codes",
				Retrieved: "2026-10-14", SHA256: sourceSHA256, License: "ODC-PDDL", Grounding: meta.Secondary,
			},
			{
				Name: "Unicode CLDR pt.xml (nomes em português)", URL: "https://github.com/unicode-org/cldr",
				Retrieved: "2026-10-14", License: "Unicode-3.0", Grounding: meta.Secondary,
			},
		},
	}
}
