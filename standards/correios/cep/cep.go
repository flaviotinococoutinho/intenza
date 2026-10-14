//intenza:paradigm data-oriented

// Package cep parses Brazilian postal codes and resolves the federative unit
// they belong to.
//
// The Correios assign CEP ranges per unit, and every range boundary falls on a
// multiple of 1 000, so the unit is a function of the first three digits: a
// 1 000-byte direct table answers it with one load, no search.
package cep

import (
	"errors"

	"github.com/flaviotinococoutinho/intenza/standards/ibge/uf"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

var ErrFormat = errors.New("cep: 8 digits, optionally 00000-000")

// CEP is a postal code held as its eight-digit number.
type CEP uint32

// Parse accepts "01310100" or "01310-100".
func Parse(s string) (CEP, error) {
	var n uint32
	digits := 0
	for i := range len(s) {
		c := s[i]
		if c == '-' && i == 5 && len(s) == 9 {
			continue
		}
		if c < '0' || c > '9' {
			return 0, ErrFormat
		}
		n = n*10 + uint32(c-'0')
		digits++
	}
	if digits != 8 {
		return 0, ErrFormat
	}
	return CEP(n), nil
}

// String renders 00000-000.
func (c CEP) String() string {
	var b [9]byte
	n := uint32(c)
	for i := 8; i >= 0; i-- {
		if i == 5 {
			b[i] = '-'
			continue
		}
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[:])
}

// UF returns the federative unit of the code; ok is false for unassigned ranges.
func (c CEP) UF() (uf.UF, bool) {
	u := byPrefix[uint32(c)/100_000]
	return u, u != 0
}

// ranges transcribed from the Correios table of CEP ranges per unit, as
// [first, last] three-digit prefixes.
var ranges = [...]struct {
	lo, hi uint16
	u      uf.UF
}{
	{10, 199, uf.SP},
	{200, 289, uf.RJ},
	{290, 299, uf.ES},
	{300, 399, uf.MG},
	{400, 489, uf.BA},
	{490, 499, uf.SE},
	{500, 569, uf.PE},
	{570, 579, uf.AL},
	{580, 589, uf.PB},
	{590, 599, uf.RN},
	{600, 639, uf.CE},
	{640, 649, uf.PI},
	{650, 659, uf.MA},
	{660, 688, uf.PA},
	{689, 689, uf.AP},
	{690, 692, uf.AM},
	{693, 693, uf.RR},
	{694, 698, uf.AM},
	{699, 699, uf.AC},
	{700, 727, uf.DF},
	{728, 729, uf.GO},
	{730, 736, uf.DF},
	{737, 767, uf.GO},
	{768, 769, uf.RO},
	{770, 779, uf.TO},
	{780, 788, uf.MT},
	{790, 799, uf.MS},
	{800, 879, uf.PR},
	{880, 899, uf.SC},
	{900, 999, uf.RS},
}

var byPrefix = func() (t [1000]uf.UF) {
	for _, r := range ranges {
		for p := r.lo; p <= r.hi; p++ {
			t[p] = r.u
		}
	}
	return t
}()

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:           "correios-cep",
		Code:         "Correios CEP",
		Title:        "Código de Endereçamento Postal — faixas por unidade da federação",
		Authority:    meta.Correios,
		Kind:         meta.IntervalMap,
		Edition:      "tabela de faixas de CEP por UF",
		Package:      "correios/cep",
		Structure:    "CEP como uint32; UF por tabela direta de 1000 bytes no prefixo de três dígitos",
		Verification: "faixas sem sobreposição; capitais conhecidas caem na UF certa; prefixos sem faixa (000–009, 789) ficam sem UF",
		Sources: []meta.Source{
			{
				Name: "Correios — faixas de CEP por UF (transcrição)", URL: "https://www.correios.com.br",
				Retrieved: "2026-10-14", License: "dados públicos", Grounding: meta.Reconstructed,
			},
		},
	}
}
