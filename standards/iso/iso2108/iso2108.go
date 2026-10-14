//intenza:paradigm functional-core

// Package iso2108 validates and converts International Standard Book Numbers.
//
// ISBN-13 is a GTIN-13 inside the Bookland prefixes 978 and 979, so its check
// digit comes from the GTIN package: the taxonomy is visible in the imports.
// ISBN-10 uses weights 10..1 mod 11; because 11 is prime it detects every
// single substitution and every adjacent transposition, which the tests prove.
package iso2108

import (
	"errors"

	"github.com/flaviotinococoutinho/intenza/standards/gs1/gtin"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

var (
	ErrLength = errors.New("iso2108: ISBN has 10 or 13 characters")
	ErrChar   = errors.New("iso2108: invalid character")
	ErrCheck  = errors.New("iso2108: check digit mismatch")
	ErrPrefix = errors.New("iso2108: ISBN-13 must start with 978 or 979")
	ErrNo10   = errors.New("iso2108: only 978 ISBNs have a 10-digit form")
)

// CheckDigit10 computes the ISBN-10 check character ('0'–'9' or 'X') of nine digits.
func CheckDigit10(body string) (byte, error) {
	if len(body) != 9 {
		return 0, ErrLength
	}
	sum := 0
	for i := range 9 {
		d := int(body[i]) - '0'
		if d < 0 || d > 9 {
			return 0, ErrChar
		}
		sum += (10 - i) * d
	}
	r := (11 - sum%11) % 11
	if r == 10 {
		return 'X', nil
	}
	return byte('0' + r), nil
}

func compact(s string) ([13]byte, int, error) {
	var b [13]byte
	n := 0
	for i := range len(s) {
		c := s[i]
		switch {
		case c == '-' || c == ' ':
			continue
		case c >= '0' && c <= '9', c == 'X' || c == 'x':
			if n == len(b) {
				return b, 0, ErrLength
			}
			if c == 'x' {
				c = 'X'
			}
			b[n] = c
			n++
		default:
			return b, 0, ErrChar
		}
	}
	return b, n, nil
}

// Normalize returns the compact form (digits, uppercase X) of a valid ISBN-10 or ISBN-13.
func Normalize(s string) (string, error) {
	b, n, err := compact(s)
	if err != nil {
		return "", err
	}
	switch n {
	case 10:
		for _, c := range b[:9] {
			if c == 'X' {
				return "", ErrChar
			}
		}
		want, _ := CheckDigit10(string(b[:9]))
		if want != b[9] {
			return "", ErrCheck
		}
	case 13:
		if string(b[:3]) != "978" && string(b[:3]) != "979" {
			return "", ErrPrefix
		}
		if _, err := gtin.Normalize(string(b[:13])); err != nil {
			if errors.Is(err, gtin.ErrCheck) {
				return "", ErrCheck
			}
			return "", ErrChar
		}
	default:
		return "", ErrLength
	}
	return string(b[:n]), nil
}

// Valid reports whether s is a valid ISBN-10 or ISBN-13.
func Valid(s string) bool {
	_, err := Normalize(s)
	return err == nil
}

// To13 converts any valid ISBN to its 13-digit form.
func To13(s string) (string, error) {
	n, err := Normalize(s)
	if err != nil || len(n) == 13 {
		return n, err
	}
	body := "978" + n[:9]
	d, err := gtin.CheckDigit(body)
	if err != nil {
		return "", err
	}
	return body + string(d), nil
}

// To10 converts a valid 978 ISBN to its 10-digit form.
func To10(s string) (string, error) {
	n, err := Normalize(s)
	if err != nil || len(n) == 10 {
		return n, err
	}
	if n[:3] != "978" {
		return "", ErrNo10
	}
	d, _ := CheckDigit10(n[3:12])
	return n[3:12] + string(d), nil
}

// Brazilian reports whether the ISBN carries a Brazilian registration group
// (978-85, and 978-65 since the 85 block ran out).
func Brazilian(s string) bool {
	n, err := To13(s)
	return err == nil && (n[:5] == "97885" || n[:5] == "97865")
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "iso-2108",
		Code:      "ISO 2108",
		Title:     "Information and documentation — International standard book number (ISBN)",
		Authority: meta.ISO,
		Kind:      meta.Identifier,
		Edition:   "ISO 2108:2017",
		Package:   "iso/iso2108",
		Structure: "mod 11 com pesos 10..1 para o ISBN-10; ISBN-13 delega ao GTIN-13 (prefixos Bookland)",
		Verification: "prova exaustiva: o ISBN-10 detecta toda substituição e toda transposição adjacente; " +
			"exemplos do manual da agência internacional; ida e volta 10 ↔ 13",
		Sources: []meta.Source{
			{
				Name: "International ISBN Agency — ISBN Users' Manual", URL: "https://www.isbn-international.org/content/isbn-users-manual",
				Retrieved: "2026-10-14", License: "especificação pública", Grounding: meta.Algorithmic,
			},
		},
	}
}
