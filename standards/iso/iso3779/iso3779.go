//intenza:paradigm functional-core

// Package iso3779 reads vehicle identification numbers: 17 characters split in
// WMI (ISO 3780, manufacturer), VDS (descriptor) and VIS (indicator).
//
// Two facts are kept apart on purpose. The syntax (alphabet without I, O, Q)
// is ISO 3779 everywhere. The check digit in position 9 is mandatory in North
// America (49 CFR 565) and informative elsewhere, Brazil included, so it is a
// separate predicate and never part of Valid.
package iso3779

import (
	"errors"

	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

var (
	ErrLength = errors.New("iso3779: VIN has 17 characters")
	ErrChar   = errors.New("iso3779: VIN uses A-Z and 0-9 without I, O and Q")
)

// translit maps a byte to its 49 CFR 565 value, or 0xFF when the byte is not
// allowed in a VIN. One table answers both questions.
var translit = func() (t [256]byte) {
	for i := range t {
		t[i] = 0xFF
	}
	for c := byte('0'); c <= '9'; c++ {
		t[c] = c - '0'
	}
	vals := "12345678_12345_7_923456789" // A..Z; '_' marks I, O, Q
	for i := range len(vals) {
		if vals[i] != '_' {
			t['A'+i] = vals[i] - '0'
			t['a'+i] = vals[i] - '0'
		}
	}
	return t
}()

var weights = [17]int{8, 7, 6, 5, 4, 3, 2, 10, 0, 9, 8, 7, 6, 5, 4, 3, 2}

// Valid reports whether s has the ISO 3779 syntax.
func Valid(s string) bool { return check(s) == nil }

func check(s string) error {
	if len(s) != 17 {
		return ErrLength
	}
	for i := range 17 {
		if translit[s[i]] == 0xFF {
			return ErrChar
		}
	}
	return nil
}

// CheckDigit computes the position-9 check character ('0'–'9' or 'X').
func CheckDigit(s string) (byte, error) {
	if err := check(s); err != nil {
		return 0, err
	}
	sum := 0
	for i := range 17 {
		sum += int(translit[s[i]]) * weights[i]
	}
	if r := sum % 11; r != 10 {
		return byte('0' + r), nil
	}
	return 'X', nil
}

// CheckDigitValid reports whether position 9 holds the computed check digit.
func CheckDigitValid(s string) bool {
	d, err := CheckDigit(s)
	return err == nil && (s[8] == d || (d == 'X' && s[8] == 'x'))
}

// WMI is the world manufacturer identifier (positions 1–3).
func WMI(s string) string { return part(s, 0, 3) }

// VDS is the vehicle descriptor section (positions 4–9).
func VDS(s string) string { return part(s, 3, 9) }

// VIS is the vehicle indicator section (positions 10–17).
func VIS(s string) string { return part(s, 9, 17) }

func part(s string, i, j int) string {
	if check(s) != nil {
		return ""
	}
	return s[i:j]
}

// Region is the continent the WMI was assigned to (ISO 3780).
type Region uint8

const (
	UnknownRegion Region = iota
	Africa
	Asia
	Europe
	NorthAmerica
	Oceania
	SouthAmerica
)

var regionNames = [...]string{"", "Africa", "Asia", "Europe", "North America", "Oceania", "South America"}

func (r Region) String() string { return regionNames[r] }

// RegionOf returns the region of the first WMI character.
func RegionOf(s string) Region {
	if check(s) != nil {
		return UnknownRegion
	}
	switch c := s[0] | 0x20; {
	case c >= 'a' && c <= 'h':
		return Africa
	case c >= 'j' && c <= 'r':
		return Asia
	case c >= 's' && c <= 'z':
		return Europe
	case c >= '1' && c <= '5':
		return NorthAmerica
	case c == '6' || c == '7':
		return Oceania
	case c == '8' || c == '9':
		return SouthAmerica
	}
	return UnknownRegion
}

// Brazilian reports whether the WMI falls in a block assigned to Brazil
// (9A–9E and 93–99).
func Brazilian(s string) bool {
	if check(s) != nil || s[0] != '9' {
		return false
	}
	c := s[1] &^ 0x20
	return (c >= 'A' && c <= 'E') || (s[1] >= '3' && s[1] <= '9')
}

// modelYear maps position 10 to the first year of its 30-year cycle.
var modelYear = func() (t [256]int16) {
	letters := "ABCDEFGHJKLMNPRSTVWXY"
	for i := range len(letters) {
		t[letters[i]] = int16(1980 + i)
		t[letters[i]|0x20] = int16(1980 + i)
	}
	for d := byte('1'); d <= '9'; d++ {
		t[d] = int16(2001 + int(d-'1'))
	}
	return t
}()

// ModelYears returns both candidate model years encoded in position 10; the
// code repeats every 30 years and only other context picks one.
func ModelYears(s string) (first, second int, ok bool) {
	if check(s) != nil || modelYear[s[9]] == 0 {
		return 0, 0, false
	}
	y := int(modelYear[s[9]])
	return y, y + 30, true
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "iso-3779",
		Code:      "ISO 3779 / ISO 3780",
		Title:     "Road vehicles — Vehicle identification number (VIN) and world manufacturer identifier (WMI)",
		Authority: meta.ISO,
		Kind:      meta.Identifier,
		Edition:   "ISO 3779:2009 e ISO 3780:2009; dígito verificador conforme 49 CFR 565 (EUA)",
		Package:   "iso/iso3779",
		Structure: "uma tabela de 256 bytes faz transliteração e validação de alfabeto; pesos fixos; ano-modelo por tabela",
		Verification: "exemplo oficial com dígito X; prova exaustiva de que toda substituição de um caractere muda o dígito; " +
			"alfabeto proibido (I, O, Q) recusado",
		Sources: []meta.Source{
			{
				Name: "49 CFR 565 — VIN requirements (check digit)", URL: "https://www.ecfr.gov/current/title-49/subtitle-B/chapter-V/part-565",
				Retrieved: "2026-10-14", License: "domínio público (EUA)", Grounding: meta.Algorithmic,
			},
			{
				Name: "faixas de WMI por continente e blocos do Brasil (ISO 3780, fontes secundárias)", URL: "",
				Retrieved: "2026-10-14", License: "fatos", Grounding: meta.Reconstructed,
			},
		},
	}
}
