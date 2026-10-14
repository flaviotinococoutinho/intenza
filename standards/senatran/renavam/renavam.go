//intenza:paradigm functional-core

// Package renavam validates the Registro Nacional de Veículos Automotores
// number: eleven digits (older nine-digit numbers are left-padded with zeros)
// whose last digit is a weighted mod-11 check over the first ten.
//
// There is no public normative text with the algorithm; it is reconstructed
// from independent implementations that agree with each other, and the test
// vectors come from those implementations rather than from this code.
package renavam

import (
	"errors"

	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

var (
	ErrLength = errors.New("renavam: 9 to 11 digits")
	ErrChar   = errors.New("renavam: digits only")
	ErrCheck  = errors.New("renavam: check digit mismatch")
)

var weights = [10]int{3, 2, 9, 8, 7, 6, 5, 4, 3, 2}

// CheckDigit computes the check digit of the first ten digits.
func CheckDigit(body string) (byte, error) {
	if len(body) != 10 {
		return 0, ErrLength
	}
	sum := 0
	for i := range 10 {
		d := int(body[i]) - '0'
		if d < 0 || d > 9 {
			return 0, ErrChar
		}
		sum += d * weights[i]
	}
	d := 11 - sum%11
	if d >= 10 {
		d = 0
	}
	return byte('0' + d), nil
}

// Normalize returns the eleven-digit form of a valid RENAVAM.
func Normalize(s string) (string, error) {
	if len(s) < 9 || len(s) > 11 {
		return "", ErrLength
	}
	var b [11]byte
	pad := 11 - len(s)
	for i := range pad {
		b[i] = '0'
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return "", ErrChar
		}
		b[pad+i] = s[i]
	}
	d, _ := CheckDigit(string(b[:10]))
	if d != b[10] {
		return "", ErrCheck
	}
	if pad == 0 {
		return s, nil
	}
	return string(b[:]), nil
}

// Valid reports whether s is a valid RENAVAM.
func Valid(s string) bool {
	_, err := Normalize(s)
	return err == nil
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "senatran-renavam",
		Code:      "SENATRAN RENAVAM",
		Title:     "Registro Nacional de Veículos Automotores — dígito verificador",
		Authority: meta.SENATRAN,
		Kind:      meta.Identifier,
		Edition:   "regra em uso pelos DETRANs; sem texto normativo público",
		Package:   "senatran/renavam",
		Structure: "soma ponderada (3,2,9,8,7,6,5,4,3,2) mod 11 numa passada; números de 9 dígitos completados à esquerda",
		Verification: "vetores de duas implementações independentes (pacote numbersBR do R e implementação Python publicada) " +
			"que concordam entre si, incluindo o caso do resto que vira 0",
		Sources: []meta.Source{
			{
				Name: "numbersBR (CRAN) — RENAVAN", URL: "https://rdrr.io/cran/numbersBR/src/R/RENAVAN.R",
				Retrieved: "2026-10-14", License: "MIT", Grounding: meta.Reconstructed,
			},
			{
				Name: "validar_renavam (gist público)", URL: "https://gist.github.com/biazzotto/226d241fa310d8acf016683e5c34e811",
				Retrieved: "2026-10-14", License: "público", Grounding: meta.Reconstructed,
			},
		},
	}
}
