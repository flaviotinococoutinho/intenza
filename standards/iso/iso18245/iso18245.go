//intenza:paradigm data-oriented

// Package iso18245 is the merchant category code (MCC) scheme of ISO 18245:
// four-digit codes grouped in ranges by line of business.
//
// The code itself is the value: an MCC is a uint16 in [0, 9999]. Membership
// and the position of its description come from a rank dictionary over the
// 10 000-slot universe (1.25 KB of bits); the range group from a 100-entry
// table indexed by code/100, since every ISO 18245 boundary is a multiple of 100.
package iso18245

//go:generate go run ../../internal/gen iso18245

import (
	"errors"
	"iter"
	"strconv"

	"github.com/flaviotinococoutinho/intenza/standards/internal/fold"
	"github.com/flaviotinococoutinho/intenza/standards/internal/succinct"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

// MCC is a merchant category code. Use Lookup or Parse to obtain a valid one.
type MCC uint16

// ErrUnknown is returned for codes not assigned in the list.
var ErrUnknown = errors.New("iso18245: unknown merchant category code")

// Lookup returns the MCC if the code is assigned.
func Lookup(code int) (MCC, bool) {
	if code < 0 || code > 9999 {
		return 0, false
	}
	_, ok := codeRank.Index(uint32(code))
	return MCC(code), ok
}

// Parse accepts the four-digit form ("5945").
func Parse(s string) (MCC, error) {
	if len(s) != 4 {
		return 0, ErrUnknown
	}
	n, ok := succinct.Digits(s)
	if !ok {
		return 0, ErrUnknown
	}
	m, ok := Lookup(int(n))
	if !ok {
		return 0, ErrUnknown
	}
	return m, nil
}

func (m MCC) index() (int, bool) { return codeRank.Index(uint32(m)) }

// Valid reports whether the code is assigned.
func (m MCC) Valid() bool {
	_, ok := m.index()
	return ok
}

// Code is the numeric value.
func (m MCC) Code() int { return int(m) }

// String is the zero-padded four-digit form.
func (m MCC) String() string {
	s := strconv.Itoa(int(m))
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

// Description is the English description.
func (m MCC) Description() string {
	i, ok := m.index()
	if !ok {
		return ""
	}
	return nameEN[nameENOff[i]:nameENOff[i+1]]
}

// DescriptionPT is the curated Portuguese description; ok is false when the
// code has not been translated yet.
func (m MCC) DescriptionPT() (string, bool) {
	i, ok := m.index()
	if !ok || namePTOff[i] == namePTOff[i+1] {
		return "", false
	}
	return namePT[namePTOff[i]:namePTOff[i+1]], true
}

// Group is the ISO 18245 range the code belongs to.
func (m MCC) Group() Group {
	if int(m) > 9999 {
		return 0
	}
	return groupOf[m/100]
}

// MarshalText encodes the four-digit form.
func (m MCC) MarshalText() ([]byte, error) {
	if !m.Valid() {
		return nil, ErrUnknown
	}
	return []byte(m.String()), nil
}

// UnmarshalText decodes the four-digit form.
func (m *MCC) UnmarshalText(b []byte) error {
	v, err := Parse(string(b))
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// All yields every assigned code in ascending order.
func All() iter.Seq[MCC] {
	return func(yield func(MCC) bool) {
		for i := range count {
			if !yield(MCC(codes[i])) {
				return
			}
		}
	}
}

// Search appends the codes whose number or descriptions have every word of q
// as a prefix ("hobby toy" → 5945). Case and accent insensitive.
func Search(dst []MCC, q string) []MCC {
	var buf [64]byte
	set, ok := tokens.Match(fold.Append(buf[:0], q))
	if !ok {
		return dst
	}
	for i := range set.All() {
		dst = append(dst, MCC(codes[i]))
	}
	return dst
}

// Group is one of the ISO 18245 ranges. The zero value is invalid.
type Group uint8

// The ranges, in code order.
const (
	AgriculturalServices Group = iota + 1
	ContractedServices
	Airlines
	CarRental
	Lodging
	TransportationServices
	UtilityServices
	RetailOutletServices
	ClothingStores
	MiscellaneousStores
	BusinessServices
	ProfessionalServices
	GovernmentServices
)

var groups = [...]struct {
	lo, hi   uint16
	name, pt string
}{
	{},
	{0, 1499, "Agricultural services", "Serviços agrícolas"},
	{1500, 2999, "Contracted services", "Serviços contratados"},
	{3000, 3299, "Airlines", "Companhias aéreas"},
	{3300, 3499, "Car rental", "Locação de veículos"},
	{3500, 3999, "Lodging", "Hospedagem"},
	{4000, 4799, "Transportation services", "Serviços de transporte"},
	{4800, 4999, "Utility services", "Serviços de utilidade pública"},
	{5000, 5599, "Retail outlet services", "Varejo"},
	{5600, 5699, "Clothing stores", "Lojas de vestuário"},
	{5700, 7299, "Miscellaneous stores", "Lojas diversas"},
	{7300, 7999, "Business services", "Serviços empresariais"},
	{8000, 8999, "Professional services and membership organizations", "Serviços profissionais e associações"},
	{9000, 9999, "Government services", "Serviços governamentais"},
}

// groupOf maps code/100 to its group: 100 bytes, one load.
var groupOf = func() (t [100]Group) {
	for g := AgriculturalServices; g <= GovernmentServices; g++ {
		for h := groups[g].lo / 100; h <= groups[g].hi/100; h++ {
			t[h] = g
		}
	}
	return t
}()

// Range is the inclusive code interval of the group.
func (g Group) Range() (lo, hi int) {
	if g < AgriculturalServices || g > GovernmentServices {
		return 0, -1
	}
	return int(groups[g].lo), int(groups[g].hi)
}

// Name is the English name of the range.
func (g Group) Name() string {
	if g < AgriculturalServices || g > GovernmentServices {
		return ""
	}
	return groups[g].name
}

// NamePT is the Portuguese name of the range.
func (g Group) NamePT() string {
	if g < AgriculturalServices || g > GovernmentServices {
		return ""
	}
	return groups[g].pt
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "iso-18245",
		Code:      "ISO 18245",
		Title:     "Retail financial services — Merchant category codes",
		Authority: meta.ISO,
		Kind:      meta.Classification,
		Edition:   "ISO 18245:2023; códigos e descrições de fontes públicas secundárias",
		Package:   "iso/iso18245",
		Structure: "o código é o valor; rank sobre 10 000 posições (1,25 KB) para pertinência e descrição; grupo por tabela de 100 bytes (código/100)",
		Verification: "faixas sem buracos nem sobreposição cobrem 0–9999 (prova exaustiva); toda tradução aponta para código existente; " +
			"descrições conferidas por amostragem contra a publicação do IRS",
		Sources: []meta.Source{
			{
				Name: "greggles/mcc-codes (IRS e USDA)", URL: "https://github.com/greggles/mcc-codes",
				Retrieved: "2026-10-14", SHA256: sourceSHA256, License: "Unlicense", Grounding: meta.Secondary,
			},
			{
				Name: "faixas de grupo e traduções curadas", URL: "",
				Retrieved: "2026-10-14", License: "MIT (este repositório)", Grounding: meta.Reconstructed,
			},
		},
	}
}
