package schema_test

import (
	"errors"
	"math/rand/v2"
	"testing"

	"pgregory.net/rapid"

	"github.com/flaviotinococoutinho/intenza/internal/domain/schema"
	"github.com/flaviotinococoutinho/intenza/internal/domain/schema/schemagen"
	"github.com/flaviotinococoutinho/intenza/internal/domain/taxonomy"
)

func tcg(t testing.TB) schema.Schema {
	t.Helper()
	c, err := taxonomy.Default.Lookup("collectibles.tcg")
	if err != nil {
		t.Fatal(err)
	}
	return c.Schema()
}

func TestParseFactsValidatesEveryFieldAtOnce(t *testing.T) {
	s := tcg(t)
	facts, err := s.ParseFacts(map[string]any{
		"game": "pokemon", "set_code": "BS1", "card_number": "4/102", "language": "PT",
		"condition": "NM", "grading_company": "PSA", "grade": 9.5, "first_edition": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if facts["language"].Str != "pt" || facts["grade"].Num != 95 {
		t.Fatalf("normalization: %+v %+v", facts["language"], facts["grade"])
	}
	_, err = s.ParseFacts(map[string]any{"game": "chess", "grade": 9.3, "grading_company": "PSA", "ghost": 1})
	var errs schema.Errors
	if !errors.As(err, &errs) || !errors.Is(err, schema.ErrInvalid) {
		t.Fatalf("want schema.Errors, got %v", err)
	}
	got := map[string]bool{}
	for _, fe := range errs {
		got[fe.Field] = true
	}
	for _, f := range []string{"game", "grade", "ghost", "set_code", "card_number", "language", "condition"} {
		if !got[f] {
			t.Errorf("missing error for %s in %v", f, errs)
		}
	}
}

func TestRequiredIf(t *testing.T) {
	s := tcg(t)
	base := map[string]any{"game": "magic", "set_code": "LEA", "card_number": "232", "language": "en", "condition": "LP"}
	base["grading_company"] = "none"
	if _, err := s.ParseFacts(base); err != nil {
		t.Fatalf("ungraded card needs no grade: %v", err)
	}
	base["grading_company"] = "BGS"
	if _, err := s.ParseFacts(base); err == nil {
		t.Fatal("graded card needs a grade")
	}
}

func TestNormativeKindsValidateThroughStandards(t *testing.T) {
	c, _ := taxonomy.Default.Lookup("auto-parts.body-panels")
	s := c.Schema()
	f, _ := s.Field("donor_renavam")
	if _, err := f.ParseValue("75320797786"); err == nil {
		t.Fatal("RENAVAM with a wrong check digit must not become evidence")
	}
	if v, err := f.ParseValue("639884962"); err != nil || v.Str != "00639884962" {
		t.Fatalf("RENAVAM normalization: %v %v", v, err)
	}
	vin, _ := s.Field("donor_vin")
	if _, err := vin.ParseValue("1M8GDM9AXKP04278O"); err == nil {
		t.Fatal("VIN with O is invalid")
	}
}

func TestAdherenceAndHardConstraints(t *testing.T) {
	s := tcg(t)
	facts, _ := s.ParseFacts(map[string]any{
		"game": "pokemon", "set_code": "BS1", "card_number": "4", "language": "pt",
		"condition": "LP", "grading_company": "PSA", "grade": "9.0",
	})
	cs, err := s.ParseConstraints(map[string]any{
		"grade":     map[string]any{"min": 9.5, "required": true, "weight": 3.0},
		"condition": map[string]any{"in": []any{"NM", "LP"}},
		"language":  "pt",
	})
	if err != nil {
		t.Fatal(err)
	}
	score, compliant, detail := schema.Adherence(cs, facts)
	if compliant || score != 2.0/5.0 || len(detail) != 3 {
		t.Fatalf("score %v compliant %v detail %+v", score, compliant, detail)
	}
	if d := s.Describe(cs[1]); d != "grade ∈ [9.5, +∞]" {
		t.Fatalf("describe: %s", d)
	}
}

func TestConferenceOnlyAboutEvidentiaryTerms(t *testing.T) {
	s := tcg(t)
	raw := map[string]any{"game": "pokemon", "set_code": "BS1", "card_number": "4", "language": "pt", "condition": "NM", "grading_company": "none"}
	promised, _ := s.ParseFacts(raw)
	raw["condition"] = "HP"
	observed, _ := s.ParseFacts(raw)
	got, err := s.Conference(promised, observed, []string{"condition", "language"})
	if err != nil || len(got) != 1 || got[0] != (schema.Discrepancy{Field: "condition", Promised: "NM", Observed: "HP"}) {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.Conference(promised, observed, []string{"finish"}); err == nil {
		t.Fatal("a claim about something the terms did not fix is not a discrepancy")
	}
}

// Property, over every leaf of the real catalog: whatever the generator draws
// parses back, the item satisfies predicates derived from itself, and
// conference against itself finds nothing.
func TestGeneratedItemsRoundTripOnEveryLeaf(t *testing.T) {
	var leaves []schema.Schema
	for c := range taxonomy.Default.All() {
		if c.Leaf() {
			leaves = append(leaves, c.Schema())
		}
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := leaves[rapid.IntRange(0, len(leaves)-1).Draw(rt, "leaf")]
		seed := rapid.Uint64().Draw(rt, "seed")
		r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
		raw := schemagen.Facts(r, s, 0.5)
		facts, err := s.ParseFacts(raw)
		if err != nil {
			rt.Fatalf("%s: generated item rejected: %v (%v)", s.Category, err, raw)
		}
		cs, err := s.ParseConstraints(schemagen.Constraints(r, s, raw, 0.6, 0))
		if err != nil {
			rt.Fatalf("%s: generated constraints rejected: %v", s.Category, err)
		}
		if score, ok, _ := schema.Adherence(cs, facts); score != 1 || !ok {
			rt.Fatalf("%s: item fails predicates derived from itself (%v)", s.Category, score)
		}
		disc, err := s.Conference(facts, facts, keysOf(facts, s))
		if err != nil || len(disc) != 0 {
			rt.Fatalf("%s: self-conference found %v (%v)", s.Category, disc, err)
		}
	})
}

func keysOf(f schema.Facts, s schema.Schema) []string {
	var out []string
	for _, k := range s.Evidentiary() {
		if _, ok := f[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// Property: adherence is a weighted share, always in [0, 1].
func TestAdherenceIsBounded(t *testing.T) {
	s := tcg(t)
	rapid.Check(t, func(rt *rapid.T) {
		seed := rapid.Uint64().Draw(rt, "seed")
		r := rand.New(rand.NewPCG(seed, 7))
		item := schemagen.Facts(r, s, 0.7)
		facts, err := s.ParseFacts(item)
		if err != nil {
			rt.Fatal(err)
		}
		other := schemagen.Facts(r, s, 0.7)
		cs, err := s.ParseConstraints(schemagen.Constraints(r, s, other, 0.8, 0.5))
		if err != nil {
			rt.Fatal(err)
		}
		if score, _, _ := schema.Adherence(cs, facts); score < 0 || score > 1 {
			rt.Fatalf("score %v", score)
		}
	})
}
