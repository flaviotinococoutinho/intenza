//intenza:paradigm data-oriented

// Package uf is the list of Brazilian federative units with their IBGE codes,
// macro-regions and ISO 3166-2:BR subdivision codes.
//
// The macro-region is not stored: in the IBGE scheme it is the first digit of
// the two-digit unit code. In other words,
// the unit code (11–17 Norte, 21–29 Nordeste, 31–35 Sudeste, 41–43 Sul,
// 50–53 Centro-Oeste), so Region is one division.
package uf

import (
	"iter"

	"github.com/flaviotinococoutinho/intenza/standards/internal/succinct"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

// UF is a federative unit, in alphabetical order of its abbreviation.
type UF uint8

// The 27 units.
const (
	AC UF = iota + 1
	AL
	AM
	AP
	BA
	CE
	DF
	ES
	GO
	MA
	MG
	MS
	MT
	PA
	PB
	PE
	PI
	PR
	RJ
	RN
	RO
	RR
	RS
	SC
	SE
	SP
	TO
)

const count = 27

const abbrevs = "ACALAMAPBACEDFESGOMAMGMSMTPAPBPEPIPRRJRNRORRRSSCSESPTO"

const iso = "BR-ACBR-ALBR-AMBR-APBR-BABR-CEBR-DFBR-ESBR-GOBR-MABR-MGBR-MSBR-MTBR-PABR-PBBR-PEBR-PIBR-PRBR-RJBR-RNBR-ROBR-RRBR-RSBR-SCBR-SEBR-SPBR-TO"

var ibge = [count]uint8{12, 27, 13, 16, 29, 23, 53, 32, 52, 21, 31, 50, 51, 15, 25, 26, 22, 41, 33, 24, 11, 14, 43, 42, 28, 35, 17}

// names holds the official names separated by '|'; offsets are derived once.
const names = "Acre|Alagoas|Amazonas|Amapá|Bahia|Ceará|Distrito Federal|Espírito Santo|Goiás|Maranhão|Minas Gerais|" +
	"Mato Grosso do Sul|Mato Grosso|Pará|Paraíba|Pernambuco|Piauí|Paraná|Rio de Janeiro|Rio Grande do Norte|Rondônia|" +
	"Roraima|Rio Grande do Sul|Santa Catarina|Sergipe|São Paulo|Tocantins|"

var nameOff = func() (off [count + 1]uint16) {
	n := 0
	for i := range len(names) {
		if names[i] == '|' {
			n++
			off[n] = uint16(i + 1)
		}
	}
	return off
}()

// byAbbrev and byIBGE are direct-address tables: one load per lookup.
var byAbbrev, byIBGE = func() (a [26 * 26]UF, b [100]UF) {
	for u := AC; u <= TO; u++ {
		k, _ := succinct.Alpha2(u.Abbrev())
		a[k] = u
		b[ibge[u-1]] = u
	}
	return a, b
}()

// ByAbbrev finds a unit by its abbreviation ("SP"), in either case.
func ByAbbrev(s string) (UF, bool) {
	k, ok := succinct.Alpha2(s)
	if !ok || byAbbrev[k] == 0 {
		return 0, false
	}
	return byAbbrev[k], true
}

// ByIBGE finds a unit by its two-digit IBGE code (35 for São Paulo).
func ByIBGE(code int) (UF, bool) {
	if code < 0 || code >= len(byIBGE) || byIBGE[code] == 0 {
		return 0, false
	}
	return byIBGE[code], true
}

// Valid reports whether u is one of the 27 units.
func (u UF) Valid() bool { return u >= AC && u <= TO }

// Abbrev is the two-letter abbreviation.
func (u UF) Abbrev() string {
	if !u.Valid() {
		return ""
	}
	i := 2 * int(u-1)
	return abbrevs[i : i+2]
}

// String is the abbreviation.
func (u UF) String() string { return u.Abbrev() }

// IBGE is the two-digit unit code.
func (u UF) IBGE() int {
	if !u.Valid() {
		return 0
	}
	return int(ibge[u-1])
}

// Name is the official name.
func (u UF) Name() string {
	if !u.Valid() {
		return ""
	}
	return names[nameOff[u-1] : nameOff[u]-1]
}

// ISO31662 is the ISO 3166-2 subdivision code ("BR-SP").
func (u UF) ISO31662() string {
	if !u.Valid() {
		return ""
	}
	i := 5 * int(u-1)
	return iso[i : i+5]
}

// Region is the IBGE macro-region: the first digit of the unit code.
func (u UF) Region() Region {
	if !u.Valid() {
		return 0
	}
	return Region(ibge[u-1] / 10)
}

// All yields the units in alphabetical order.
func All() iter.Seq[UF] {
	return func(yield func(UF) bool) {
		for u := AC; u <= TO; u++ {
			if !yield(u) {
				return
			}
		}
	}
}

// Region is an IBGE macro-region, numbered as in the IBGE scheme.
type Region uint8

// The five macro-regions.
const (
	Norte Region = iota + 1
	Nordeste
	Sudeste
	Sul
	CentroOeste
)

var regionNames = [...]string{"", "Norte", "Nordeste", "Sudeste", "Sul", "Centro-Oeste"}

// Name is the region name.
func (r Region) Name() string {
	if r < Norte || r > CentroOeste {
		return ""
	}
	return regionNames[r]
}

// String is the region name.
func (r Region) String() string { return r.Name() }

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:           "ibge-uf",
		Code:         "IBGE DTB — UF",
		Title:        "Divisão Territorial Brasileira: unidades da federação e grandes regiões (com ISO 3166-2:BR)",
		Authority:    meta.IBGE,
		Kind:         meta.CodeList,
		Edition:      "DTB vigente; códigos de UF estáveis desde a criação do Tocantins (1988)",
		Package:      "ibge/uf",
		Structure:    "tabelas de endereçamento direto (676 e 100 bytes); região derivada do primeiro dígito do código, sem tabela",
		Verification: "prova exaustiva: as 27 UFs fazem ida e volta por sigla e por código; região do dígito confere com a lista oficial",
		Sources: []meta.Source{
			{
				Name: "IBGE — códigos dos municípios e das UFs", URL: "https://www.ibge.gov.br/explica/codigos-dos-municipios.php",
				Retrieved: "2026-10-14", License: "dados públicos", Grounding: meta.Reconstructed,
			},
		},
	}
}
