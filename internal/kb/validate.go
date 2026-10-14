package kb

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Severity of a finding. Errors break the build; warnings are debt.
type Severity string

const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
)

// Finding is one problem found while checking the base against the facts.
type Finding struct {
	Severity Severity
	Subject  string
	Message  string
}

func (f Finding) String() string { return fmt.Sprintf("%s %s: %s", f.Severity, f.Subject, f.Message) }

// RiskStatus says whether the evidence matches the nature of the solution.
type RiskStatus string

const (
	Mitigated RiskStatus = "mitigated"
	Exposed   RiskStatus = "exposed"
)

// Report is the outcome of Validate.
type Report struct {
	Findings []Finding
	Risks    map[string]RiskStatus
	ByBasis  map[Basis]int
}

// Errors counts findings with error severity.
func (r Report) Errors() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == SevError {
			n++
		}
	}
	return n
}

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

type checker struct {
	base      Base
	facts     Facts
	concepts  map[string]Concept
	claims    map[string]Claim
	standards map[string]Standard
	findings  []Finding
}

func (c *checker) add(sev Severity, subject, format string, args ...any) {
	c.findings = append(c.findings, Finding{Severity: sev, Subject: subject, Message: fmt.Sprintf(format, args...)})
}

// Validate checks the declared knowledge against the facts of the world.
// It is deterministic: findings come out sorted.
func Validate(b Base, f Facts) Report {
	c := &checker{base: b, facts: f, concepts: map[string]Concept{}, claims: map[string]Claim{}, standards: map[string]Standard{}}
	for _, s := range f.Standards {
		c.standards[s.ID] = s
	}
	c.indexConcepts()
	c.checkConcepts()
	c.checkStandards()
	c.indexClaims()
	c.checkClaims()
	risks := c.checkRisks()
	c.checkPackages()

	slices.SortFunc(c.findings, func(a, b Finding) int {
		if a.Severity != b.Severity {
			return strings.Compare(string(a.Severity), string(b.Severity))
		}
		if a.Subject != b.Subject {
			return strings.Compare(a.Subject, b.Subject)
		}
		return strings.Compare(a.Message, b.Message)
	})
	byBasis := map[Basis]int{}
	for _, cl := range b.Claims {
		byBasis[cl.Basis]++
	}
	return Report{Findings: c.findings, Risks: risks, ByBasis: byBasis}
}

func (c *checker) indexConcepts() {
	for _, k := range c.base.Concepts {
		subj := "concept:" + k.ID
		if !idPattern.MatchString(k.ID) {
			c.add(SevError, subj, "id fora do padrão kebab-case")
		}
		if _, dup := c.concepts[k.ID]; dup {
			c.add(SevError, subj, "id duplicado")
		}
		c.concepts[k.ID] = k
	}
}

func (c *checker) ref(subj, rel, id string) (Concept, bool) {
	k, ok := c.concepts[id]
	if !ok {
		c.add(SevError, subj, "%s aponta para conceito inexistente %q", rel, id)
	}
	return k, ok
}

func (c *checker) checkConcepts() {
	phases := map[string]int{}
	created := map[string]bool{}
	for _, k := range c.base.Concepts {
		if k.Category == Event {
			for _, id := range k.Creates {
				created[id] = true
			}
		}
	}
	for _, k := range c.base.Concepts {
		subj := "concept:" + k.ID
		if _, ok := metacategories[k.Category]; !ok {
			c.add(SevError, subj, "metacategoria desconhecida %q", k.Category)
			continue
		}
		if strings.TrimSpace(k.Label) == "" || strings.TrimSpace(k.Definition) == "" {
			c.add(SevError, subj, "rótulo e definição são obrigatórios")
		}
		c.checkPlacement(subj, k)
		switch k.Category {
		case Role, Subkind:
			c.checkIdentity(subj, k)
		case Kind:
			if k.Specializes != "" {
				if sup, ok := c.ref(subj, "specializes", k.Specializes); ok && sup.Category != Kind {
					c.add(SevError, subj, "kind só especializa kind")
				}
			}
		case Phase:
			if k.PhaseOf == "" {
				c.add(SevError, subj, "fase precisa de phase_of")
			} else if bearer, ok := c.ref(subj, "phase_of", k.PhaseOf); ok {
				if slices.Contains([]Metacategory{Phase, Event, Datatype, Information}, bearer.Category) {
					c.add(SevError, subj, "%s (%s) não tem ciclo de vida", bearer.ID, bearer.Category)
				}
				phases[k.PhaseOf]++
			}
		case Relator:
			distinct := map[string]bool{}
			for _, id := range k.Mediates {
				if _, ok := c.ref(subj, "mediates", id); ok {
					distinct[id] = true
				}
			}
			if len(distinct) < 2 {
				c.add(SevError, subj, "relator depende existencialmente de pelo menos dois relata")
			}
			if k.FoundedBy == "" {
				c.add(SevWarning, subj, "relator sem evento fundador (founded_by)")
			} else if ev, ok := c.ref(subj, "founded_by", k.FoundedBy); ok && ev.Category != Event {
				c.add(SevError, subj, "founded_by precisa ser um evento")
			}
		case Quality:
			if k.InheresIn == "" {
				c.add(SevError, subj, "qualidade precisa de portador (inheres_in)")
			} else {
				c.ref(subj, "inheres_in", k.InheresIn)
			}
		case HighOrderType:
			if k.Classifies == "" {
				c.add(SevError, subj, "tipo de alta ordem precisa dizer o que classifica")
			} else {
				c.ref(subj, "classifies", k.Classifies)
			}
		case Event:
			if len(k.Participants) == 0 {
				c.add(SevError, subj, "evento sem participantes")
			}
			for _, id := range k.Participants {
				c.ref(subj, "participants", id)
			}
			for _, id := range k.Creates {
				c.ref(subj, "creates", id)
			}
		case Commitment, SocialObject:
			if !created[k.ID] {
				c.add(SevWarning, subj, "objeto social sem evento que o cria (creates)")
			}
		case Collective, Information, Datatype:
		}
		if k.PartOf != "" {
			c.ref(subj, "part_of", k.PartOf)
		}
		for _, id := range k.GroundedIn {
			if _, ok := c.standards[id]; !ok {
				c.add(SevError, subj, "grounded_in aponta para norma desconhecida %q", id)
			}
		}
		for _, sym := range k.RealizedBy {
			if !c.facts.Symbols[sym] {
				c.add(SevError, subj, "realized_by aponta para símbolo inexistente %q", sym)
			}
		}
	}
	for _, k := range c.base.Concepts {
		if n, ok := phases[k.ID]; ok && n < 2 {
			c.add(SevError, "concept:"+k.ID, "fases formam partição: são precisas pelo menos duas")
		}
	}
}

// checkStandards keeps the normative chain honest: a norm nobody grounds on
// is dead weight, and a reconstructed source is debt until reviewed.
func (c *checker) checkStandards() {
	grounded := map[string]bool{}
	for _, k := range c.base.Concepts {
		for _, id := range k.GroundedIn {
			grounded[id] = true
		}
	}
	for _, s := range c.facts.Standards {
		subj := "standard:" + s.ID
		if !grounded[s.ID] {
			c.add(SevWarning, subj, "norma sem conceito ancorado (grounded_in)")
		}
		if s.Grounding == "reconstructed" {
			c.add(SevWarning, subj, "fonte reconstruída: precisa de revisão de especialista")
		}
	}
}

// checkPlacement rejects relations used on the wrong metacategory.
func (c *checker) checkPlacement(subj string, k Concept) {
	misplaced := func(rel string, set bool, allowed ...Metacategory) {
		if set && !slices.Contains(allowed, k.Category) {
			c.add(SevError, subj, "%s não se aplica a %s", rel, k.Category)
		}
	}
	misplaced("phase_of", k.PhaseOf != "", Phase)
	misplaced("mediates", len(k.Mediates) > 0, Relator)
	misplaced("founded_by", k.FoundedBy != "", Relator)
	misplaced("inheres_in", k.InheresIn != "", Quality)
	misplaced("classifies", k.Classifies != "", HighOrderType)
	misplaced("participants", len(k.Participants) > 0, Event)
	misplaced("creates", len(k.Creates) > 0, Event)
	misplaced("specializes", k.Specializes != "", Kind, Subkind, Role)
}

// checkIdentity follows specializes until a kind: anti-rigid and subordinate
// sortals borrow their identity principle from exactly one kind.
func (c *checker) checkIdentity(subj string, k Concept) {
	seen := map[string]bool{k.ID: true}
	cur := k
	for cur.Category != Kind {
		if cur.Specializes == "" {
			c.add(SevError, subj, "%s precisa especializar um kind (provedor de identidade)", k.Category)
			return
		}
		next, ok := c.ref(subj, "specializes", cur.Specializes)
		if !ok {
			return
		}
		if seen[next.ID] {
			c.add(SevError, subj, "ciclo em specializes")
			return
		}
		if k.Category == Subkind && next.Category == Role {
			c.add(SevError, subj, "subkind (rígido) não especializa role (anti-rígido)")
			return
		}
		seen[next.ID] = true
		cur = next
	}
}

func (c *checker) indexClaims() {
	for _, cl := range c.base.Claims {
		subj := "claim:" + cl.ID
		if !idPattern.MatchString(cl.ID) {
			c.add(SevError, subj, "id fora do padrão kebab-case")
		}
		if _, dup := c.claims[cl.ID]; dup {
			c.add(SevError, subj, "id duplicado")
		}
		c.claims[cl.ID] = cl
	}
}

func (c *checker) checkClaims() {
	for _, cl := range c.base.Claims {
		subj := "claim:" + cl.ID
		if strings.TrimSpace(cl.Statement) == "" {
			c.add(SevError, subj, "enunciado vazio")
		}
		if !claimKinds[cl.Kind] {
			c.add(SevError, subj, "tipo de alegação desconhecido %q", cl.Kind)
		}
		if _, ok := bases[cl.Basis]; !ok {
			c.add(SevError, subj, "base epistêmica desconhecida %q", cl.Basis)
			continue
		}
		if len(cl.About) == 0 {
			c.add(SevError, subj, "alegação precisa dizer sobre o que é (about)")
		}
		for _, id := range cl.About {
			c.ref(subj, "about", id)
		}
		for _, id := range cl.FalsifiableBy {
			if !c.facts.Experiments[id] {
				c.add(SevError, subj, "falsifiable_by aponta para experimento inexistente %q", id)
			}
		}
		c.checkEvidence(subj, cl)
	}
}

func (c *checker) checkEvidence(subj string, cl Claim) {
	var tests, symbols, measured, sources int
	for _, e := range cl.Evidence {
		set := 0
		for _, v := range []string{e.Test, e.Symbol, e.Bench, e.Experiment, e.Source} {
			if v != "" {
				set++
			}
		}
		if set != 1 {
			c.add(SevError, subj, "cada evidência aponta exatamente um localizador")
			continue
		}
		switch {
		case e.Test != "":
			if c.facts.Tests[e.Test] {
				tests++
			} else {
				c.add(SevError, subj, "teste inexistente %q", e.Test)
			}
		case e.Bench != "":
			if !c.facts.Tests[e.Bench] {
				c.add(SevError, subj, "benchmark inexistente %q", e.Bench)
			}
		case e.Symbol != "":
			if c.facts.Symbols[e.Symbol] {
				symbols++
			} else {
				c.add(SevError, subj, "símbolo inexistente %q", e.Symbol)
			}
		case e.Experiment != "":
			if !c.facts.Experiments[e.Experiment] {
				c.add(SevError, subj, "experimento inexistente %q", e.Experiment)
				continue
			}
			want := e.Expect
			if want == "" {
				want = "held"
			}
			res, ok := c.facts.Results[e.Experiment]
			switch {
			case !ok:
				c.add(SevError, subj, "experimento %q sem resultado registrado", e.Experiment)
			case res.Verdict != want:
				c.add(SevError, subj, "experimento %q terminou %q, esperado %q", e.Experiment, res.Verdict, want)
			default:
				measured++
			}
		case e.Source != "":
			sources++
		}
	}
	switch cl.Basis {
	case ByConstruction:
		if symbols == 0 {
			c.add(SevError, subj, "por construção exige um símbolo que materialize a garantia")
		}
	case ProofExhaustive, PropertyTested, ExampleTested:
		if tests == 0 {
			c.add(SevError, subj, "%s exige pelo menos um teste existente", cl.Basis)
		}
	case Measured:
		if measured == 0 {
			c.add(SevError, subj, "medido exige um experimento com o veredito esperado")
		}
	case Testimony:
		if sources == 0 {
			c.add(SevError, subj, "testemunho exige a fonte")
		}
		switch cl.Review {
		case "done":
		case "pending":
			c.add(SevWarning, subj, "testemunho ainda sem revisão de especialista")
		default:
			c.add(SevError, subj, "testemunho precisa de review: pending | done")
		}
	case Conjecture:
		if len(cl.Evidence) > 0 {
			c.add(SevWarning, subj, "conjectura com evidência: promova a base")
		}
	}
}

func (c *checker) checkRisks() map[string]RiskStatus {
	status := map[string]RiskStatus{}
	seen := map[string]bool{}
	for _, r := range c.base.Risks {
		subj := "risk:" + r.ID
		if !idPattern.MatchString(r.ID) {
			c.add(SevError, subj, "id fora do padrão kebab-case")
		}
		if seen[r.ID] {
			c.add(SevError, subj, "id duplicado")
		}
		seen[r.ID] = true
		if r.Uncertainty != Aleatoric && r.Uncertainty != Epistemic {
			c.add(SevError, subj, "incerteza deve ser aleatoric | epistemic")
		}
		if r.Solution != Deterministic && r.Solution != Stochastic && r.Solution != Hybrid {
			c.add(SevError, subj, "solução deve ser deterministic | stochastic | hybrid")
		}
		if r.Title == "" || r.Cause == "" || r.Mechanism == "" || r.Effect == "" {
			c.add(SevError, subj, "título e cadeia causa → mecanismo → efeito são obrigatórios")
		}
		if r.Severity < 1 || r.Severity > 5 || r.Likelihood < 1 || r.Likelihood > 5 {
			c.add(SevError, subj, "severidade e probabilidade vão de 1 a 5")
		}
		for _, id := range r.Experiments {
			if !c.facts.Experiments[id] {
				c.add(SevError, subj, "experimento inexistente %q", id)
			}
		}
		if len(r.MitigatedBy) == 0 {
			c.add(SevError, subj, "risco sem mitigação declarada")
		}
		var apriori, aposteriori bool
		for _, id := range r.MitigatedBy {
			cl, ok := c.claims[id]
			if !ok {
				c.add(SevError, subj, "mitigated_by aponta para alegação inexistente %q", id)
				continue
			}
			switch cl.Basis.Mode() {
			case APriori:
				apriori = true
			case APosteriori:
				aposteriori = true
			case Testimonial, Unjustified:
			}
		}
		st := Exposed
		switch r.Solution {
		case Deterministic:
			if apriori {
				st = Mitigated
			} else if aposteriori {
				c.add(SevError, subj, "erro de categoria: garantia determinística não se justifica por amostragem")
			}
		case Stochastic:
			if aposteriori {
				st = Mitigated
			} else if apriori {
				c.add(SevWarning, subj, "solução estocástica sem medição: os testes cobrem o mecanismo, não o efeito")
			}
		case Hybrid:
			if apriori && aposteriori {
				st = Mitigated
			}
		}
		status[r.ID] = st
	}
	return status
}

var forbiddenImports = []string{
	"os", "os/exec", "os/signal", "net", "net/http", "net/rpc", "syscall", "plugin",
	"database/sql", "log", "log/slog", "io/ioutil",
}

var forbiddenPrefixes = []string{
	"go.mongodb.org/", "github.com/twmb/", "github.com/redis/", "github.com/danielgtaylor/huma",
	"go.opentelemetry.io/", "github.com/prometheus/",
}

// impure reports whether a reference breaks determinism: wall clock, global
// randomness or the entropy source.
func impure(s Selector) bool {
	switch s.Path {
	case "time":
		switch s.Name {
		case "Now", "Since", "Until", "Sleep", "After", "AfterFunc", "Tick", "NewTimer", "NewTicker":
			return true
		}
	case "math/rand", "math/rand/v2":
		switch s.Name {
		case "New", "NewSource", "NewPCG", "NewChaCha8", "NewZipf", "Rand", "Source", "PCG", "ChaCha8", "Zipf":
			return false
		}
		return true
	case "crypto/rand":
		return true
	}
	return false
}

func (c *checker) checkPackages() {
	for _, p := range c.facts.Packages {
		subj := "package:" + p.Dir
		if p.Paradigm == "" {
			c.add(SevError, subj, "pacote sem paradigma declarado (//intenza:paradigm)")
			continue
		}
		if _, ok := paradigms[p.Paradigm]; !ok {
			c.add(SevError, subj, "paradigma desconhecido %q", p.Paradigm)
			continue
		}
		if !p.Paradigm.Pure() {
			continue
		}
		for _, imp := range p.Imports {
			bad := slices.Contains(forbiddenImports, imp)
			for _, pre := range forbiddenPrefixes {
				bad = bad || strings.HasPrefix(imp, pre)
			}
			if bad {
				c.add(SevError, subj, "%s é puro e não importa %s", p.Paradigm, imp)
			}
		}
		for _, s := range p.Selectors {
			if impure(s) {
				c.add(SevError, subj, "%s é puro e não usa %s.%s (%s): injete", p.Paradigm, s.Path, s.Name, s.Pos)
			}
		}
	}
}
