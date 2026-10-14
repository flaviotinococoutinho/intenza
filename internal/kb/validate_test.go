package kb

import (
	"strings"
	"testing"
)

func fixture() (Base, Facts) {
	b := Base{
		Concepts: []Concept{
			{ID: "party", Label: "Parte", Category: Kind, Definition: "d"},
			{ID: "buyer", Label: "Comprador", Category: Role, Specializes: "party", Definition: "d"},
			{ID: "seller", Label: "Vendedor", Category: Role, Specializes: "party", Definition: "d"},
			{ID: "deal", Label: "Acordo", Category: Relator, Mediates: []string{"buyer", "seller"}, FoundedBy: "chosen", Definition: "d"},
			{ID: "deal-open", Label: "Aberto", Category: Phase, PhaseOf: "deal", Definition: "d"},
			{ID: "deal-done", Label: "Feito", Category: Phase, PhaseOf: "deal", Definition: "d"},
			{ID: "chosen", Label: "Escolhido", Category: Event, Participants: []string{"buyer"}, Definition: "d"},
		},
		Claims: []Claim{
			{
				ID: "one-deal", Statement: "s", About: []string{"deal"}, Kind: Safety, Basis: ExampleTested,
				Evidence: []Evidence{{Test: "internal/x.TestOneDeal"}},
			},
			{
				ID: "fast", Statement: "s", About: []string{"deal"}, Kind: Performance, Basis: Measured,
				Evidence: []Evidence{{Experiment: "load"}},
			},
		},
		Risks: []Risk{
			{
				ID: "double-deal", Title: "t", Uncertainty: Aleatoric, Solution: Deterministic, Cause: "c", Mechanism: "m",
				Effect: "e", Severity: 3, Likelihood: 2, MitigatedBy: []string{"one-deal"},
			},
			{
				ID: "slow", Title: "t", Uncertainty: Aleatoric, Solution: Stochastic, Cause: "c", Mechanism: "m",
				Effect: "e", Severity: 3, Likelihood: 2, MitigatedBy: []string{"fast"},
			},
		},
	}
	f := Facts{
		Symbols:     map[string]bool{"internal/x.Deal": true},
		Tests:       map[string]bool{"internal/x.TestOneDeal": true},
		Experiments: map[string]bool{"load": true},
		Results:     map[string]Result{"load": {Verdict: "held"}},
		Packages:    []Package{{Dir: "internal/x", Paradigm: FunctionalCore, Imports: []string{"strings"}}},
	}
	b.Concepts[0].GroundedIn = []string{"rfb-cpf"}
	f.Standards = []Standard{{ID: "rfb-cpf", Code: "CPF", Authority: "RFB", Kind: "identifier", Grounding: "algorithmic"}}
	return b, f
}

func TestValidFixtureHasNoFindings(t *testing.T) {
	b, f := fixture()
	r := Validate(b, f)
	if len(r.Findings) != 0 {
		t.Fatalf("unexpected findings: %v", r.Findings)
	}
	if r.Risks["double-deal"] != Mitigated || r.Risks["slow"] != Mitigated {
		t.Fatalf("risk status: %v", r.Risks)
	}
}

func TestValidateCatchesOntologicalAndEpistemicErrors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Base, *Facts)
		want   string
	}{
		{"role without identity provider", func(b *Base, _ *Facts) {
			b.Concepts[1].Specializes = ""
		}, "precisa especializar um kind"},
		{"subkind over role", func(b *Base, _ *Facts) {
			b.Concepts = append(b.Concepts, Concept{ID: "vip", Label: "l", Category: Subkind, Specializes: "buyer", Definition: "d"})
		}, "subkind (rígido) não especializa role"},
		{"relator with a single relatum", func(b *Base, _ *Facts) {
			b.Concepts[3].Mediates = []string{"buyer"}
		}, "pelo menos dois relata"},
		{"phase partition of one", func(b *Base, _ *Facts) {
			b.Concepts = b.Concepts[:5]
			b.Concepts = append(b.Concepts, Concept{ID: "chosen", Label: "l", Category: Event, Participants: []string{"buyer"}, Definition: "d"})
		}, "pelo menos duas"},
		{"relation on the wrong metacategory", func(b *Base, _ *Facts) {
			b.Concepts[0].Mediates = []string{"buyer", "seller"}
		}, "mediates não se aplica a kind"},
		{"dangling reference", func(b *Base, _ *Facts) {
			b.Concepts[4].PhaseOf = "ghost"
		}, "conceito inexistente \"ghost\""},
		{"test that does not exist", func(_ *Base, f *Facts) {
			delete(f.Tests, "internal/x.TestOneDeal")
		}, "teste inexistente"},
		{"measured without a held verdict", func(_ *Base, f *Facts) {
			f.Results["load"] = Result{Verdict: "falsified"}
		}, "terminou \"falsified\""},
		{"category error: deterministic justified by sampling", func(b *Base, _ *Facts) {
			b.Risks[0].MitigatedBy = []string{"fast"}
		}, "erro de categoria"},
		{"testimony without review", func(b *Base, _ *Facts) {
			b.Claims[0].Basis = Testimony
			b.Claims[0].Evidence = []Evidence{{Source: "Lei 12.865/2013"}}
		}, "testemunho precisa de review"},
		{"evidence with two locators", func(b *Base, _ *Facts) {
			b.Claims[0].Evidence = []Evidence{{Test: "internal/x.TestOneDeal", Symbol: "internal/x.Deal"}}
		}, "exatamente um localizador"},
		{"pure package reading the wall clock", func(_ *Base, f *Facts) {
			f.Packages[0].Selectors = []Selector{{Path: "time", Name: "Now", Pos: "x.go:3"}}
		}, "não usa time.Now"},
		{"pure package doing I/O", func(_ *Base, f *Facts) {
			f.Packages[0].Imports = append(f.Packages[0].Imports, "net/http")
		}, "não importa net/http"},
		{"package without paradigm", func(_ *Base, f *Facts) {
			f.Packages[0].Paradigm = ""
		}, "sem paradigma declarado"},
		{"unknown standard", func(b *Base, _ *Facts) {
			b.Concepts[0].GroundedIn = []string{"iso-0000"}
		}, "norma desconhecida"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, f := fixture()
			tc.mutate(&b, &f)
			r := Validate(b, f)
			for _, fd := range r.Findings {
				if fd.Severity == SevError && strings.Contains(fd.Message, tc.want) {
					return
				}
			}
			t.Fatalf("want error containing %q, got %v", tc.want, r.Findings)
		})
	}
}

func TestNormativeChainWarnings(t *testing.T) {
	b, f := fixture()
	f.Standards = append(f.Standards, Standard{ID: "ibge-uf", Grounding: "reconstructed"})
	r := Validate(b, f)
	var unused, reconstructed bool
	for _, fd := range r.Findings {
		unused = unused || (fd.Subject == "standard:ibge-uf" && strings.Contains(fd.Message, "sem conceito"))
		reconstructed = reconstructed || (fd.Subject == "standard:ibge-uf" && strings.Contains(fd.Message, "reconstruída"))
	}
	if !unused || !reconstructed || r.Errors() != 0 {
		t.Fatalf("want two warnings and no error, got %v", r.Findings)
	}
}

func TestStochasticRiskWithoutMeasurementStaysExposed(t *testing.T) {
	b, f := fixture()
	b.Risks[1].MitigatedBy = []string{"one-deal"}
	r := Validate(b, f)
	if r.Risks["slow"] != Exposed {
		t.Fatalf("stochastic risk backed only by tests must stay exposed, got %s", r.Risks["slow"])
	}
	if r.Errors() != 0 {
		t.Fatalf("debt is a warning, not an error: %v", r.Findings)
	}
}

func TestGlobalRandomnessIsImpureButSeededSourcesAreNot(t *testing.T) {
	for _, s := range []Selector{{Path: "math/rand/v2", Name: "IntN"}, {Path: "crypto/rand", Name: "Reader"}} {
		if !impure(s) {
			t.Errorf("%s.%s should be impure", s.Path, s.Name)
		}
	}
	for _, s := range []Selector{{Path: "math/rand/v2", Name: "NewPCG"}, {Path: "math/rand/v2", Name: "Rand"}, {Path: "time", Name: "Duration"}} {
		if impure(s) {
			t.Errorf("%s.%s should be pure", s.Path, s.Name)
		}
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	b, f := fixture()
	r := Validate(b, f)
	first := string(Render(b, f, r))
	for range 5 {
		if got := string(Render(b, f, r)); got != first {
			t.Fatal("render must be a pure function of its inputs")
		}
	}
	for _, want := range []string{"## Estado epistêmico", "## Riscos", "## Ontologia", "```mermaid", "## Paradigmas"} {
		if !strings.Contains(first, want) {
			t.Errorf("missing section %q", want)
		}
	}
}

func TestRenderCarriesClaimNotes(t *testing.T) {
	b, f := fixture()
	if strings.Contains(string(Render(b, f, Validate(b, f))), "### Ressalvas") {
		t.Fatal("no notes, no section")
	}
	b.Claims[0].Notes = "prova o mecanismo,\n  não o efeito"
	got := string(Render(b, f, Validate(b, f)))
	if !strings.Contains(got, "### Ressalvas\n\n- **one-deal**: prova o mecanismo, não o efeito\n") {
		t.Errorf("notes not rendered:\n%s", got)
	}
}
