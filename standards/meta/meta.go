//intenza:paradigm functional-core

// Package meta describes normative references: who issues them, what kind of
// artifact they define, which edition we carry and how our copy is known to be
// faithful. It is the epistemic record shared by every SDK in this module.
package meta

// Authority is the body that issues or maintains a standard. It is the first
// rank of the taxonomic chain: authority → standard → edition → artifact.
type Authority string

const (
	ISO      Authority = "ISO"      // International Organization for Standardization
	IETF     Authority = "IETF"     // Internet Engineering Task Force
	UN       Authority = "UN"       // United Nations Statistics Division
	GS1      Authority = "GS1"      // GS1 global office
	EMVCo    Authority = "EMVCo"    // EMVCo LLC
	BCB      Authority = "BCB"      // Banco Central do Brasil
	RFB      Authority = "RFB"      // Receita Federal do Brasil
	IBGE     Authority = "IBGE"     // Instituto Brasileiro de Geografia e Estatística
	Correios Authority = "Correios" // Empresa Brasileira de Correios e Telégrafos
	SENATRAN Authority = "SENATRAN" // Secretaria Nacional de Trânsito
)

// Kind is the ontological nature of the artifact a standard defines. It
// decides the data structure and the paradigm of the SDK.
type Kind string

const (
	CodeList       Kind = "code-list"      // finite set of codes with meaning: direct addressing
	Classification Kind = "classification" // hierarchy of classes: implicit trees, interval containment
	Identifier     Kind = "identifier"     // syntax plus check digit: table-driven modular arithmetic
	Grammar        Kind = "grammar"        // formal language: hand-written automata, zero allocation
	IntervalMap    Kind = "interval-map"   // ranges mapped to classes: direct table on the prefix
)

// Grounding is how our copy of the standard is justified.
type Grounding string

const (
	Primary       Grounding = "primary"       // published by the maintenance agency itself
	Secondary     Grounding = "secondary"     // reputable compilation of the primary source
	Reconstructed Grounding = "reconstructed" // assembled from public documents; needs expert review
	Algorithmic   Grounding = "algorithmic"   // a procedure, corroborated by official vectors and properties
)

// Source is one upstream input of a dataset or algorithm.
type Source struct {
	Name      string
	URL       string
	Retrieved string // ISO 8601 date the snapshot was taken
	SHA256    string // digest of the committed extract or of the upstream file
	License   string
	Grounding Grounding
}

// Standard is the record of one normative reference implemented by an SDK.
type Standard struct {
	ID           string // stable kebab id used by the knowledge base, e.g. "iso-4217"
	Code         string // human code, e.g. "ISO 4217"
	Title        string
	Authority    Authority
	Kind         Kind
	Edition      string
	Package      string // path inside the module, e.g. "iso/iso4217"
	Structure    string // data structure and lookup cost
	Verification string // how conformance is checked
	Sources      []Source
}

// Weakest returns the weakest grounding among the sources: a chain of
// justification is only as strong as its weakest link.
func (s Standard) Weakest() Grounding {
	rank := map[Grounding]int{Algorithmic: 4, Primary: 3, Secondary: 2, Reconstructed: 1}
	weakest := Algorithmic
	for _, src := range s.Sources {
		if rank[src.Grounding] < rank[weakest] {
			weakest = src.Grounding
		}
	}
	if len(s.Sources) == 0 {
		return Reconstructed
	}
	return weakest
}
