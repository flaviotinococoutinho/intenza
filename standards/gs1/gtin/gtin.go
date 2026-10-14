//intenza:paradigm functional-core

// Package gtin validates GS1 Global Trade Item Numbers (GTIN-8, -12, -13 and
// -14) and resolves the GS1 Prefix of a number to the member organization that
// issued it.
//
// The check digit is a weighted sum mod 10 (weights 3 and 1 from the right).
// It detects every single-digit substitution and every adjacent transposition
// except between digits that differ by 5; the tests prove both by exhaustion.
// The GS1 Prefix identifies who issued the number, not where the product was
// made (GS1 says so explicitly).
package gtin

import (
	"errors"

	"github.com/flaviotinococoutinho/intenza/standards/internal/ident"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

var (
	ErrLength = errors.New("gtin: length must be 8, 12, 13 or 14 digits")
	ErrChar   = errors.New("gtin: digits only")
	ErrCheck  = errors.New("gtin: check digit mismatch")
)

// CheckDigit computes the digit that completes body (the GTIN without its last digit).
func CheckDigit(body string) (byte, error) {
	if len(body) < 7 || len(body) > 17 {
		return 0, ErrLength
	}
	sum := 0
	for i := len(body) - 1; i >= 0; i-- {
		d := int(body[i]) - '0'
		if d < 0 || d > 9 {
			return 0, ErrChar
		}
		if (len(body)-1-i)%2 == 0 {
			d *= 3
		}
		sum += d
	}
	return byte('0' + (10-sum%10)%10), nil
}

// Normalize strips spaces and hyphens and returns the digits of a valid GTIN.
func Normalize(s string) (string, error) {
	var buf [14]byte
	b, ok := ident.Compact(buf[:0], s, false)
	if !ok {
		if len(b) == cap(b) {
			return "", ErrLength
		}
		return "", ErrChar
	}
	switch len(b) {
	case 8, 12, 13, 14:
	default:
		return "", ErrLength
	}
	want, err := CheckDigit(string(b[:len(b)-1]))
	if err != nil {
		return "", err
	}
	if want != b[len(b)-1] {
		return "", ErrCheck
	}
	if len(b) == len(s) {
		return s, nil // already compact: no copy
	}
	return string(b), nil
}

// Valid reports whether s is a GTIN with a correct check digit.
func Valid(s string) bool {
	_, err := Normalize(s)
	return err == nil
}

// To14 left-pads a valid GTIN to 14 digits, the form used in databases.
func To14(s string) (string, error) {
	n, err := Normalize(s)
	if err != nil {
		return "", err
	}
	const zeros = "000000"
	return zeros[:14-len(n)] + n, nil
}

// Prefix returns the GS1 Prefix entry of a GTIN-12, -13 or -14. GTIN-12 has an
// implied leading zero; GTIN-14 carries an indicator digit before the prefix.
// GTIN-8 has its own allocation scheme and returns false.
func Prefix(s string) (PrefixEntry, bool) {
	n, err := To14(s)
	if err != nil || n[:6] == "000000" {
		return 0, false
	}
	p := int(n[1]-'0')*100 + int(n[2]-'0')*10 + int(n[3]-'0')
	return prefixTable[p], true
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "gs1-gtin",
		Code:      "GS1 GTIN",
		Title:     "Global Trade Item Number e GS1 Prefixes (GS1 General Specifications)",
		Authority: meta.GS1,
		Kind:      meta.Identifier,
		Edition:   "GS1 General Specifications vigentes; tabela de prefixos transcrita em 2026-10-14",
		Package:   "gs1/gtin",
		Structure: "soma ponderada 3/1 mod 10 em uma passada; prefixo por tabela direta de 1000 bytes",
		Verification: "prova exaustiva do que o dígito detecta (toda substituição; transposição exceto |a−b| = 5); " +
			"exemplos da GS1; cobertura total dos 1000 prefixos",
		Sources: []meta.Source{
			{
				Name: "GS1 — check digit calculation", URL: "https://www.gs1.org/services/how-calculate-check-digit-manually",
				Retrieved: "2026-10-14", License: "especificação pública", Grounding: meta.Algorithmic,
			},
			{
				Name: "GS1 — GS1 Company Prefix (tabela de GS1 Prefixes)", URL: "https://www.gs1.org/standards/id-keys/company-prefix",
				Retrieved: "2026-10-14", License: "especificação pública", Grounding: meta.Primary,
			},
		},
	}
}
