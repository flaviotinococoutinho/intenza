//intenza:paradigm functional-core

// Package cnpj validates the Cadastro Nacional da Pessoa Jurídica number,
// including the alphanumeric format the Receita Federal issues from July 2026
// (IN RFB 2.229/2024): twelve characters in [0-9A-Z] plus two numeric check
// digits.
//
// The algorithm did not change, only the alphabet: each character is worth its
// ASCII code minus 48, so digits keep their values and 'A' is 17. Old numeric
// CNPJs stay valid under the same code path.
package cnpj

import (
	"errors"

	"github.com/flaviotinococoutinho/intenza/standards/internal/ident"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

var (
	ErrLength   = errors.New("cnpj: 14 characters")
	ErrChar     = errors.New("cnpj: base in [0-9A-Z], check digits numeric")
	ErrCheck    = errors.New("cnpj: check digits mismatch")
	ErrRepeated = errors.New("cnpj: repeated characters are not issued")
)

var (
	w1 = [12]int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	w2 = [13]int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
)

func value(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9', c >= 'A' && c <= 'Z':
		return int(c) - '0', true
	}
	return 0, false
}

// CheckDigits computes the two check digits of a twelve-character base.
func CheckDigits(base string) (d1, d2 byte, err error) {
	if len(base) != 12 {
		return 0, 0, ErrLength
	}
	s1, s2 := 0, 0
	for i := range 12 {
		v, ok := value(base[i])
		if !ok {
			return 0, 0, ErrChar
		}
		s1 += v * w1[i]
		s2 += v * w2[i]
	}
	v1 := ident.Mod11(s1)
	v2 := ident.Mod11(s2 + int(v1)*w2[12])
	return '0' + v1, '0' + v2, nil
}

// Normalize returns the 14 characters of a valid CNPJ, uppercased, accepting
// the 00.000.000/0000-00 mask.
func Normalize(s string) (string, error) {
	var buf [14]byte
	b, ok := ident.Compact(buf[:0], s, true)
	switch {
	case !ok && len(b) == cap(b):
		return "", ErrLength
	case !ok:
		return "", ErrChar
	case len(b) != 14:
		return "", ErrLength
	case ident.AllSame(b):
		return "", ErrRepeated
	}
	if b[12] > '9' || b[13] > '9' {
		return "", ErrChar
	}
	d1, d2, err := CheckDigits(string(b[:12]))
	if err != nil {
		return "", err
	}
	if b[12] != d1 || b[13] != d2 {
		return "", ErrCheck
	}
	if string(b) == s {
		return s, nil
	}
	return string(b), nil
}

// Valid reports whether s is a valid numeric or alphanumeric CNPJ.
func Valid(s string) bool {
	_, err := Normalize(s)
	return err == nil
}

// Alphanumeric reports whether a valid CNPJ uses letters in its base.
func Alphanumeric(s string) bool {
	n, err := Normalize(s)
	if err != nil {
		return false
	}
	for i := range 12 {
		if n[i] >= 'A' {
			return true
		}
	}
	return false
}

// Format renders a valid CNPJ as 00.000.000/0000-00 (letters included).
func Format(s string) (string, error) {
	n, err := Normalize(s)
	if err != nil {
		return "", err
	}
	return n[0:2] + "." + n[2:5] + "." + n[5:8] + "/" + n[8:12] + "-" + n[12:14], nil
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "rfb-cnpj",
		Code:      "RFB CNPJ",
		Title:     "Cadastro Nacional da Pessoa Jurídica — formato alfanumérico e dígitos verificadores",
		Authority: meta.RFB,
		Kind:      meta.Identifier,
		Edition:   "IN RFB nº 2.229/2024 (CNPJ alfanumérico a partir de julho de 2026)",
		Package:   "rfb/cnpj",
		Structure: "valor do caractere = ASCII − 48; duas somas ponderadas mod 11 numa passada só",
		Verification: "exemplo alfanumérico publicado pela Receita (12.ABC.345/01DE-35) e exemplo numérico clássico; " +
			"o mesmo caminho valida os dois formatos",
		Sources: []meta.Source{
			{
				Name: "Receita Federal — CNPJ alfanumérico", URL: "https://www.gov.br/receitafederal/pt-br/acesso-a-informacao/acoes-e-programas/programas-e-atividades/cnpj-alfanumerico",
				Retrieved: "2026-10-14", License: "dados públicos", Grounding: meta.Algorithmic,
			},
		},
	}
}
