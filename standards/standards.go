//intenza:paradigm functional-core

// Package standards is the registry of the normative-reference SDKs in this
// module, organized as a taxonomic chain:
//
//	authority → standard → edition → artifact kind → package
//
// The artifact kind decides the paradigm and the data structure:
//
//	code-list       data-oriented   rank dictionaries over dense code universes, O(1), pointer-free
//	classification  data-oriented   implicit trees with Euler intervals, containment in two comparisons
//	identifier      functional      table-driven check digits, exhaustive proofs of what they detect
//	grammar         automata        hand-written scanners, no regexp, no allocation on the hot path
//	interval-map    data-oriented   direct tables on the significant prefix
//
// Every SDK reports its epistemic record through Spec(): the sources, how our
// copy is grounded (primary, secondary, reconstructed, algorithmic) and how its
// conformance is verified. The module has no dependencies outside the standard
// library.
package standards

import (
	"slices"
	"strings"

	"github.com/flaviotinococoutinho/intenza/standards/bcb/brcode"
	"github.com/flaviotinococoutinho/intenza/standards/correios/cep"
	"github.com/flaviotinococoutinho/intenza/standards/emvco/mpm"
	"github.com/flaviotinococoutinho/intenza/standards/gs1/gtin"
	"github.com/flaviotinococoutinho/intenza/standards/ibge/uf"
	"github.com/flaviotinococoutinho/intenza/standards/ietf/rfc8785"
	"github.com/flaviotinococoutinho/intenza/standards/ietf/rfc9562"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso18245"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso2108"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso3166"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso3779"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso4217"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso639"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso8601"
	"github.com/flaviotinococoutinho/intenza/standards/meta"
	"github.com/flaviotinococoutinho/intenza/standards/rfb/cnpj"
	"github.com/flaviotinococoutinho/intenza/standards/rfb/cpf"
	"github.com/flaviotinococoutinho/intenza/standards/senatran/renavam"
	"github.com/flaviotinococoutinho/intenza/standards/un/m49"
)

// All returns the record of every SDK, ordered by authority then code.
func All() []meta.Standard {
	all := []meta.Standard{
		iso18245.Spec(),
		iso4217.Spec(),
		iso3166.Spec(),
		iso639.Spec(),
		m49.Spec(),
		uf.Spec(),
		gtin.Spec(),
		iso2108.Spec(),
		iso3779.Spec(),
		cpf.Spec(),
		cnpj.Spec(),
		renavam.Spec(),
		cep.Spec(),
		iso8601.Spec(),
		rfc8785.Spec(),
		rfc9562.Spec(),
		mpm.Spec(),
		brcode.Spec(),
	}
	slices.SortFunc(all, func(a, b meta.Standard) int {
		if c := strings.Compare(string(a.Authority), string(b.Authority)); c != 0 {
			return c
		}
		return strings.Compare(a.Code, b.Code)
	})
	return all
}

// ByID finds a record by its knowledge-base id ("iso-4217").
func ByID(id string) (meta.Standard, bool) {
	for _, s := range All() {
		if s.ID == id {
			return s, true
		}
	}
	return meta.Standard{}, false
}
