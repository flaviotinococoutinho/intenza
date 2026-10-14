//intenza:paradigm functional-core

// Package cpf validates the Cadastro de Pessoas Físicas number of the Receita
// Federal: nine digits plus two mod-11 check digits.
//
// Mod 11 with the 0/1 → 0 collapse is not a perfect code: a change that moves
// a remainder between 0 and 1 keeps the digit. The tests measure how often
// that lets a single substitution through instead of pretending it never does.
package cpf

import (
	"errors"

	"github.com/flaviotinococoutinho/intenza/standards/internal/ident"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

var (
	ErrLength   = errors.New("cpf: 11 digits")
	ErrChar     = errors.New("cpf: digits only")
	ErrCheck    = errors.New("cpf: check digits mismatch")
	ErrRepeated = errors.New("cpf: repeated digits are not issued")
)

// CheckDigits computes the two check digits of a nine-digit body.
func CheckDigits(body string) (d1, d2 byte, err error) {
	if len(body) != 9 {
		return 0, 0, ErrLength
	}
	s1, s2 := 0, 0
	for i := range 9 {
		d := int(body[i]) - '0'
		if d < 0 || d > 9 {
			return 0, 0, ErrChar
		}
		s1 += d * (10 - i)
		s2 += d * (11 - i)
	}
	v1 := ident.Mod11(s1)
	v2 := ident.Mod11(s2 + 2*int(v1))
	return '0' + v1, '0' + v2, nil
}

// Normalize returns the 11 digits of a valid CPF, accepting the usual mask.
func Normalize(s string) (string, error) {
	var buf [11]byte
	b, ok := ident.Compact(buf[:0], s, false)
	switch {
	case !ok && len(b) == cap(b):
		return "", ErrLength
	case !ok:
		return "", ErrChar
	case len(b) != 11:
		return "", ErrLength
	case ident.AllSame(b):
		return "", ErrRepeated
	}
	d1, d2, _ := CheckDigits(string(b[:9]))
	if b[9] != d1 || b[10] != d2 {
		return "", ErrCheck
	}
	if len(s) == 11 {
		return s, nil
	}
	return string(b), nil
}

// Valid reports whether s is a valid CPF.
func Valid(s string) bool {
	_, err := Normalize(s)
	return err == nil
}

// Format renders a valid CPF as 000.000.000-00.
func Format(s string) (string, error) {
	n, err := Normalize(s)
	if err != nil {
		return "", err
	}
	return n[0:3] + "." + n[3:6] + "." + n[6:9] + "-" + n[9:11], nil
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "rfb-cpf",
		Code:      "RFB CPF",
		Title:     "Cadastro de Pessoas Físicas — dígitos verificadores",
		Authority: meta.RFB,
		Kind:      meta.Identifier,
		Edition:   "regra de cálculo publicada pela Receita Federal",
		Package:   "rfb/cpf",
		Structure: "duas somas ponderadas mod 11 numa passada só, sem alocação",
		Verification: "exemplo conhecido; sequências repetidas recusadas; taxa de detecção de substituição medida por amostragem " +
			"com o mecanismo da colisão 0/1 explicado",
		Sources: []meta.Source{
			{
				Name: "Receita Federal — cálculo do dígito verificador do CPF", URL: "https://www.gov.br/receitafederal",
				Retrieved: "2026-10-14", License: "dados públicos", Grounding: meta.Algorithmic,
			},
		},
	}
}
