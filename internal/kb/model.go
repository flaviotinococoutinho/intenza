//intenza:paradigm functional-core

// Package kb models the project's knowledge base as data: what exists
// (ontology), what is claimed and how it is known (epistemics), what can go
// wrong (risks) and which programming paradigm governs each package.
//
// Everything here is pure: it validates and renders values handed in by the
// imperative shell (package source), so the same input always yields the same
// report.
package kb

// Metacategory is the ontological nature of a concept, after UFO (Unified
// Foundational Ontology) and multi-level modeling.
type Metacategory string

const (
	Kind          Metacategory = "kind"          // rigid sortal: supplies identity (party, item)
	Subkind       Metacategory = "subkind"       // rigid specialization of a kind (person, company)
	Role          Metacategory = "role"          // anti-rigid, relationally dependent (buyer, seller)
	Phase         Metacategory = "phase"         // anti-rigid, intrinsically conditioned (lifecycle state)
	Relator       Metacategory = "relator"       // existentially depends on 2+ relata (agreement, escrow)
	Commitment    Metacategory = "commitment"    // social commitment with validity (proposal)
	SocialObject  Metacategory = "social-object" // created by a speech act (intent, rating)
	Quality       Metacategory = "quality"       // inheres in a bearer, valued in a quality structure
	HighOrderType Metacategory = "type"          // instances are themselves types (category)
	Collective    Metacategory = "collective"    // plural whole with uniform members (niche)
	Event         Metacategory = "event"         // perdurant: happens, has participants
	Information   Metacategory = "information"   // information artifact (schema, terms snapshot)
	Datatype      Metacategory = "datatype"      // value without identity (money, duration, identifiers)
)

var metacategories = map[Metacategory]string{
	Kind:          "tipo rígido que dá identidade",
	Subkind:       "especialização rígida de um tipo",
	Role:          "papel anti-rígido, depende de relação",
	Phase:         "fase anti-rígida do ciclo de vida",
	Relator:       "relator: existe entre duas ou mais partes",
	Commitment:    "compromisso social com validade",
	SocialObject:  "objeto social criado por ato de fala",
	Quality:       "qualidade que inere num portador",
	HighOrderType: "tipo de alta ordem: instâncias são tipos",
	Collective:    "coletivo de membros uniformes",
	Event:         "evento (perdurante)",
	Information:   "artefato de informação",
	Datatype:      "valor sem identidade",
}

// Concept is a node of the domain ontology. Relations are typed fields so the
// YAML stays readable and every relation can be checked.
type Concept struct {
	ID           string       `yaml:"id"`
	Label        string       `yaml:"label"`
	Category     Metacategory `yaml:"category"`
	Definition   string       `yaml:"definition"`
	Specializes  string       `yaml:"specializes,omitempty"`
	PhaseOf      string       `yaml:"phase_of,omitempty"`
	Mediates     []string     `yaml:"mediates,omitempty"`
	InheresIn    string       `yaml:"inheres_in,omitempty"`
	Classifies   string       `yaml:"classifies,omitempty"`
	FoundedBy    string       `yaml:"founded_by,omitempty"`
	Participants []string     `yaml:"participants,omitempty"`
	Creates      []string     `yaml:"creates,omitempty"`
	PartOf       string       `yaml:"part_of,omitempty"`
	GroundedIn   []string     `yaml:"grounded_in,omitempty"`
	RealizedBy   []string     `yaml:"realized_by,omitempty"`
}

// Basis is how a claim is known. The order of the constants is the order of
// epistemic strength used in reports.
type Basis string

const (
	ByConstruction  Basis = "by-construction"  // the type system/structure makes the violation unrepresentable
	ProofExhaustive Basis = "proof-exhaustive" // a finite domain enumerated entirely by a test
	PropertyTested  Basis = "property-tested"  // universal claim sampled by generated inputs (inductive)
	ExampleTested   Basis = "example-tested"   // chosen examples
	Measured        Basis = "measured"         // survived a falsification attempt (chaos/load experiment)
	Testimony       Basis = "testimony"        // accepted on the authority of a norm or regulator
	Conjecture      Basis = "conjecture"       // not yet supported: epistemic debt
)

// Mode groups bases by the source of justification.
type Mode string

const (
	APriori     Mode = "a-priori"     // holds for every input in scope: deterministic knowledge
	APosteriori Mode = "a-posteriori" // corroborated by observing the running system: stochastic knowledge
	Testimonial Mode = "testimonial"  // borrowed from an authority
	Unjustified Mode = "unjustified"
)

var bases = map[Basis]struct {
	mode     Mode
	strength int
	label    string
}{
	ByConstruction:  {APriori, 7, "por construção"},
	ProofExhaustive: {APriori, 6, "prova exaustiva"},
	PropertyTested:  {APriori, 5, "teste de propriedade"},
	ExampleTested:   {APriori, 4, "teste de exemplo"},
	Measured:        {APosteriori, 3, "medido"},
	Testimony:       {Testimonial, 2, "testemunho"},
	Conjecture:      {Unjustified, 1, "conjectura"},
}

// Mode returns the justification mode of the basis.
func (b Basis) Mode() Mode { return bases[b].mode }

// Strength orders bases from conjecture (1) to by-construction (7).
func (b Basis) Strength() int { return bases[b].strength }

// ClaimKind is what a claim is about.
type ClaimKind string

const (
	Safety      ClaimKind = "safety"      // something bad never happens
	Liveness    ClaimKind = "liveness"    // something good eventually happens
	Integrity   ClaimKind = "integrity"   // data stays correct and tamper-evident
	Performance ClaimKind = "performance" // latency/throughput bounds
	Regulatory  ClaimKind = "regulatory"  // compliance with an external norm
	Economic    ClaimKind = "economic"    // market design property
)

var claimKinds = map[ClaimKind]bool{
	Safety: true, Liveness: true, Integrity: true, Performance: true, Regulatory: true, Economic: true,
}

// Evidence points at whatever justifies a claim. Exactly one locator is set.
type Evidence struct {
	Test       string `yaml:"test,omitempty"`   // dir.TestName
	Symbol     string `yaml:"symbol,omitempty"` // dir.Identifier
	Bench      string `yaml:"bench,omitempty"`  // dir.BenchmarkName
	Experiment string `yaml:"experiment,omitempty"`
	Expect     string `yaml:"expect,omitempty"` // expected verdict, default "held"
	Source     string `yaml:"source,omitempty"` // norm, regulation, paper
	URL        string `yaml:"url,omitempty"`
}

// Claim is an assertion about the system together with its justification.
type Claim struct {
	ID            string     `yaml:"id"`
	Statement     string     `yaml:"statement"`
	About         []string   `yaml:"about"`
	Kind          ClaimKind  `yaml:"kind"`
	Basis         Basis      `yaml:"basis"`
	Evidence      []Evidence `yaml:"evidence,omitempty"`
	Review        string     `yaml:"review,omitempty"` // testimony: pending | done
	FalsifiableBy []string   `yaml:"falsifiable_by,omitempty"`
	Notes         string     `yaml:"notes,omitempty"`
}

// Uncertainty is the nature of what makes a risk uncertain.
type Uncertainty string

const (
	Aleatoric Uncertainty = "aleatoric" // irreducible randomness: latency, crashes, arrivals
	Epistemic Uncertainty = "epistemic" // lack of knowledge: unknown paths, curator error, norms
)

// Solution is the nature of the answer to a risk.
type Solution string

const (
	Deterministic Solution = "deterministic" // removes the failure mode; known a priori
	Stochastic    Solution = "stochastic"    // lowers probability/impact; known a posteriori
	Hybrid        Solution = "hybrid"        // a deterministic floor plus a stochastic bound
)

// Risk is an entry of the risk register, written as a causal chain.
type Risk struct {
	ID          string      `yaml:"id"`
	Title       string      `yaml:"title"`
	Uncertainty Uncertainty `yaml:"uncertainty"`
	Solution    Solution    `yaml:"solution"`
	Cause       string      `yaml:"cause"`
	Mechanism   string      `yaml:"mechanism"`
	Effect      string      `yaml:"effect"`
	Signals     []string    `yaml:"signals,omitempty"`
	Severity    int         `yaml:"severity"`
	Likelihood  int         `yaml:"likelihood"`
	MitigatedBy []string    `yaml:"mitigated_by"`
	Experiments []string    `yaml:"experiments,omitempty"`
}

// Base is the declared knowledge: the files under knowledge/.
type Base struct {
	Concepts []Concept
	Claims   []Claim
	Risks    []Risk
}

// Paradigm is the programming paradigm that governs a package.
type Paradigm string

const (
	FunctionalCore  Paradigm = "functional-core"  // pure functions over immutable values
	Automata        Paradigm = "automata"         // explicit, table-driven state machines
	DataOriented    Paradigm = "data-oriented"    // static tables, struct-of-arrays, pointer-free
	Probabilistic   Paradigm = "probabilistic"    // stochastic algorithms with injected randomness
	Generative      Paradigm = "generative"       // programs that write programs (staged computation)
	Reactive        Paradigm = "reactive"         // event-driven message passing
	CSP             Paradigm = "csp"              // goroutines and channels as the unit of design
	ImperativeShell Paradigm = "imperative-shell" // I/O, side effects, wiring
)

var paradigms = map[Paradigm]struct {
	pure  bool
	label string
}{
	FunctionalCore:  {true, "núcleo funcional"},
	Automata:        {true, "autômatos"},
	DataOriented:    {true, "orientado a dados"},
	Probabilistic:   {true, "probabilístico"},
	Generative:      {false, "generativo"},
	Reactive:        {false, "reativo"},
	CSP:             {false, "CSP"},
	ImperativeShell: {false, "casca imperativa"},
}

// Pure reports whether packages of this paradigm must stay deterministic:
// no I/O, no wall clock, no global randomness.
func (p Paradigm) Pure() bool { return paradigms[p].pure }

// Facts is what the world says: the code, the experiments and their results.
type Facts struct {
	Symbols     map[string]bool // "dir.Name" for top-level declarations
	Tests       map[string]bool // "dir.TestX", "dir.BenchmarkX", "dir.FuzzX"
	Packages    []Package
	Experiments map[string]bool
	Results     map[string]Result
	Standards   map[string]bool
}

// Package summarizes one Go package for the paradigm rules.
type Package struct {
	Dir       string
	Name      string
	Paradigm  Paradigm
	Imports   []string
	Selectors []Selector // references to members of imported packages
}

// Selector is a reference such as time.Now found in a package.
type Selector struct {
	Path string // import path
	Name string
	Pos  string
}

// Result is the last recorded run of an experiment.
type Result struct {
	Verdict  string `json:"verdict"`
	Finished string `json:"finished_at"`
}
