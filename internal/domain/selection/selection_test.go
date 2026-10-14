package selection

import (
	"math"
	"slices"
	"testing"
	"time"

	"pgregory.net/rapid"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func genCandidates(t *rapid.T, n int) []Candidate {
	out := make([]Candidate, n)
	for i := range out {
		out[i] = Candidate{
			ProposalID:   string(rune('a'+i%26)) + string(rune('0'+i/26)),
			PriceMinor:   rapid.Int64Range(1_000, 500_000).Draw(t, "price"),
			Reputation:   float64(rapid.IntRange(0, 100).Draw(t, "rep")) / 100,
			Adherence:    float64(rapid.IntRange(0, 10).Draw(t, "adh")) / 10,
			Compliant:    rapid.Bool().Draw(t, "compliant"),
			LeadTimeDays: rapid.IntRange(1, 30).Draw(t, "lead"),
			SubmittedAt:  t0.Add(time.Duration(rapid.IntRange(0, 1000).Draw(t, "at")) * time.Second),
		}
	}
	return out
}

func genWeights(t *rapid.T) Weights {
	return Weights{
		Price:      float64(rapid.IntRange(1, 10).Draw(t, "wp")),
		Reputation: float64(rapid.IntRange(1, 10).Draw(t, "wr")),
		Adherence:  float64(rapid.IntRange(1, 10).Draw(t, "wa")),
		LeadTime:   float64(rapid.IntRange(1, 10).Draw(t, "wl")),
	}
}

func ids(rs []Ranked) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ProposalID
	}
	return out
}

// Property: arrival order never changes the ranking.
func TestRankingIsPermutationInvariant(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cands := genCandidates(rt, rapid.IntRange(1, 12).Draw(rt, "n"))
		w := genWeights(rt)
		for _, m := range []Method{WeightedSum, TOPSIS} {
			a, _ := Rank(cands, w, m)
			shuffled := slices.Clone(cands)
			perm := rapid.Permutation(shuffled).Draw(rt, "perm")
			b, _ := Rank(perm, w, m)
			if !slices.Equal(ids(a), ids(b)) {
				rt.Fatalf("%s: order depends on input order:\n%v\n%v", m, ids(a), ids(b))
			}
		}
	})
}

// Property: with positive weights, a compliant proposal that dominates
// another always ranks above it, under both methods.
func TestDominatedNeverRanksAboveItsDominator(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cands := genCandidates(rt, rapid.IntRange(2, 10).Draw(rt, "n"))
		for i := range cands {
			cands[i].Compliant = true
		}
		w := genWeights(rt)
		for _, m := range []Method{WeightedSum, TOPSIS} {
			rs, _ := Rank(cands, w, m)
			pos := map[string]int{}
			for _, r := range rs {
				pos[r.ProposalID] = r.Rank
			}
			for _, a := range cands {
				for _, b := range cands {
					if dominates(a, b) && pos[a.ProposalID] > pos[b.ProposalID] {
						rt.Fatalf("%s: %+v dominates %+v but ranks %d vs %d", m, a, b, pos[a.ProposalID], pos[b.ProposalID])
					}
				}
			}
		}
	})
}

// Property: scores live in [0, 1], weighted-sum contributions add up to the
// score, compliant proposals come first and dominated ones are off the front.
func TestScoresContributionsAndPareto(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cands := genCandidates(rt, rapid.IntRange(1, 10).Draw(rt, "n"))
		rs, err := Rank(cands, genWeights(rt), WeightedSum)
		if err != nil {
			rt.Fatal(err)
		}
		seenNonCompliant := false
		for _, r := range rs {
			if r.Score < -1e-12 || r.Score > 1+1e-12 {
				rt.Fatalf("score %v", r.Score)
			}
			sum := r.Contributions[0] + r.Contributions[1] + r.Contributions[2] + r.Contributions[3]
			if math.Abs(sum-r.Score) > 1e-9 {
				rt.Fatalf("contributions %v sum to %v, score %v", r.Contributions, sum, r.Score)
			}
			if !r.Compliant {
				seenNonCompliant = true
			} else if seenNonCompliant {
				rt.Fatal("compliant ranked after non-compliant")
			}
			for _, o := range rs {
				if dominates(o.Candidate, r.Candidate) && r.Pareto {
					rt.Fatalf("%s is dominated but on the Pareto front", r.ProposalID)
				}
			}
		}
	})
}

func TestExampleMarket(t *testing.T) {
	cands := []Candidate{
		{ProposalID: "cheap-new-seller", PriceMinor: 90_000, Reputation: 0.55, Adherence: 1, Compliant: true, LeadTimeDays: 7, SubmittedAt: t0},
		{ProposalID: "trusted", PriceMinor: 110_000, Reputation: 0.97, Adherence: 1, Compliant: true, LeadTimeDays: 3, SubmittedAt: t0},
		{ProposalID: "off-spec", PriceMinor: 60_000, Reputation: 0.9, Adherence: 0.4, Compliant: false, LeadTimeDays: 2, SubmittedAt: t0},
	}
	rs, err := Rank(cands, DefaultWeights, WeightedSum)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(rs); !slices.Equal(got, []string{"trusted", "cheap-new-seller", "off-spec"}) {
		t.Fatalf("order %v", got)
	}
	if _, err := Rank(cands, Weights{}, WeightedSum); err == nil {
		t.Fatal("all-zero weights")
	}
}
