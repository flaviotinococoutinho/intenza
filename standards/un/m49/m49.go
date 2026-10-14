//intenza:paradigm data-oriented

// Package m49 is the UN M49 standard for statistical areas: world, regions,
// sub-regions and intermediate regions. It is the classification above
// ISO 3166-1, so a country can be asked whether it lies in "Latin America".
//
// The hierarchy is stored as an implicit tree with Euler-tour numbering:
// a.Contains(b) is two integer comparisons, with no pointer chasing and no
// recursion.
package m49

//go:generate go run ../../internal/gen m49

import (
	"iter"

	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

// Area is a handle to an M49 area. The zero value is invalid.
type Area uint16

// ByCode finds an area by its three-digit numeric code (19 for the Americas).
func ByCode(code int) (Area, bool) {
	if code < 0 || code > 999 {
		return 0, false
	}
	i, ok := codeRank.Index(uint32(code))
	return Area(i + 1), ok
}

// Valid reports whether a points at an area.
func (a Area) Valid() bool { return a > 0 && int(a) <= count }

func (a Area) i() int { return int(a) - 1 }

// Code is the numeric code.
func (a Area) Code() int {
	if !a.Valid() {
		return 0
	}
	return int(codes[a.i()])
}

// Name is the English name.
func (a Area) Name() string {
	if !a.Valid() {
		return ""
	}
	return nameEN[nameENOff[a.i()]:nameENOff[a.i()+1]]
}

// NamePT is the Brazilian Portuguese name.
func (a Area) NamePT() string {
	if !a.Valid() {
		return ""
	}
	return namePT[namePTOff[a.i()]:namePTOff[a.i()+1]]
}

// Depth is 0 for the world, 1 for regions, 2 for sub-regions, 3 for intermediate regions.
func (a Area) Depth() int {
	if !a.Valid() {
		return -1
	}
	return int(depth[a.i()])
}

// Parent is the enclosing area; the world has none.
func (a Area) Parent() (Area, bool) {
	if !a.Valid() || parent[a.i()] == 0 {
		return 0, false
	}
	return Area(parent[a.i()]), true
}

// Contains reports whether b lies inside a (an area contains itself).
func (a Area) Contains(b Area) bool {
	if !a.Valid() || !b.Valid() {
		return false
	}
	return pre[a.i()] <= pre[b.i()] && pre[b.i()] < post[a.i()]
}

// Ancestors yields a and every enclosing area up to the world.
func (a Area) Ancestors() iter.Seq[Area] {
	return func(yield func(Area) bool) {
		for cur := a; cur.Valid(); {
			if !yield(cur) {
				return
			}
			p, ok := cur.Parent()
			if !ok {
				return
			}
			cur = p
		}
	}
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "un-m49",
		Code:      "UN M49",
		Title:     "Standard country or area codes for statistical use",
		Authority: meta.UN,
		Kind:      meta.Classification,
		Edition:   "revisão vigente na UNSD, via datasets/country-codes",
		Package:   "un/m49",
		Structure: "árvore implícita com numeração de Euler (pre/post): contenção em duas comparações; rank sobre 1000 códigos",
		Verification: "toda área tem pai até o Mundo; todo país de ISO 3166-1 cai numa área existente; " +
			"contenção conferida contra a cadeia de pais",
		Sources: []meta.Source{
			{
				Name: "datasets/country-codes (compilação UNSD/ISO)", URL: "https://github.com/datasets/country-codes",
				Retrieved: "2026-10-14", SHA256: sourceSHA256, License: "ODC-PDDL", Grounding: meta.Secondary,
			},
			{
				Name: "Unicode CLDR pt.xml (nomes em português)", URL: "https://github.com/unicode-org/cldr",
				Retrieved: "2026-10-14", License: "Unicode-3.0", Grounding: meta.Secondary,
			},
		},
	}
}
