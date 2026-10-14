//intenza:paradigm functional-core

// Package selection ranks revealed proposals by several criteria at once:
// total price, seller reputation, adherence to the buyer's constraints and
// lead time. Two classic multi-criteria methods are offered (weighted sum and
// TOPSIS), plus the Pareto front, and the order is a pure function of the
// inputs: reproducible, independent of arrival order and explainable term by
// term.
package selection

import (
	"errors"
	"math"
	"slices"
	"strings"
	"time"
)

// Method is the aggregation method.
type Method string

const (
	WeightedSum Method = "weighted-sum"
	TOPSIS      Method = "topsis"
)

// Weights of the four criteria; they are normalized to sum 1.
type Weights struct {
	Price      float64 `json:"price"`
	Reputation float64 `json:"reputation"`
	Adherence  float64 `json:"adherence"`
	LeadTime   float64 `json:"lead_time"`
}

// DefaultWeights favors price without letting it decide alone.
var DefaultWeights = Weights{Price: 0.4, Reputation: 0.25, Adherence: 0.25, LeadTime: 0.1}

var ErrWeights = errors.New("selection: weights must be non-negative and not all zero")

func (w Weights) normalized() (Weights, error) {
	sum := w.Price + w.Reputation + w.Adherence + w.LeadTime
	if w.Price < 0 || w.Reputation < 0 || w.Adherence < 0 || w.LeadTime < 0 || sum <= 0 || math.IsNaN(sum) || math.IsInf(sum, 0) {
		return Weights{}, ErrWeights
	}
	return Weights{w.Price / sum, w.Reputation / sum, w.Adherence / sum, w.LeadTime / sum}, nil
}

func (w Weights) vector() [4]float64 {
	return [4]float64{w.Price, w.Reputation, w.Adherence, w.LeadTime}
}

// Candidate is a revealed proposal as seen by the ranking.
type Candidate struct {
	ProposalID   string    `json:"proposal_id"`
	PriceMinor   int64     `json:"price_minor"`
	Reputation   float64   `json:"reputation"` // posterior mean in [0, 1]
	Adherence    float64   `json:"adherence"`  // weighted share of satisfied constraints
	Compliant    bool      `json:"compliant"`  // every hard constraint holds
	LeadTimeDays int       `json:"lead_time_days"`
	SubmittedAt  time.Time `json:"submitted_at"`
}

// benefit is +1 for criteria to maximize and -1 for costs.
var benefit = [4]float64{-1, +1, +1, -1}

func (c Candidate) values() [4]float64 {
	return [4]float64{float64(c.PriceMinor), c.Reputation, c.Adherence, float64(c.LeadTimeDays)}
}

// Ranked is a candidate with its score and the share each criterion contributed.
type Ranked struct {
	Candidate
	Rank          int        `json:"rank"`
	Score         float64    `json:"score"`
	Pareto        bool       `json:"pareto"`
	Contributions [4]float64 `json:"contributions"` // price, reputation, adherence, lead time (weighted sum only)
}

// Rank orders candidates. Compliant ones come first; inside each group the
// score decides and ties fall back to price, lead time, submission time and
// id, a total order.
func Rank(cands []Candidate, w Weights, m Method) ([]Ranked, error) {
	nw, err := w.normalized()
	if err != nil {
		return nil, err
	}
	out := make([]Ranked, len(cands))
	for i, c := range cands {
		out[i].Candidate = c
	}
	switch m {
	case WeightedSum:
		weightedSum(out, nw.vector())
	case TOPSIS:
		topsis(out, nw.vector())
	default:
		return nil, errors.New("selection: unknown method " + string(m))
	}
	for i := range out {
		out[i].Pareto = true
		for j := range out {
			if i != j && dominates(out[j].Candidate, out[i].Candidate) {
				out[i].Pareto = false
				break
			}
		}
	}
	slices.SortFunc(out, compare)
	for i := range out {
		out[i].Rank = i + 1
	}
	return out, nil
}

func compare(a, b Ranked) int {
	switch {
	case a.Compliant != b.Compliant:
		if a.Compliant {
			return -1
		}
		return 1
	case a.Score != b.Score:
		if a.Score > b.Score {
			return -1
		}
		return 1
	case a.PriceMinor != b.PriceMinor:
		if a.PriceMinor < b.PriceMinor {
			return -1
		}
		return 1
	case a.LeadTimeDays != b.LeadTimeDays:
		return a.LeadTimeDays - b.LeadTimeDays
	case !a.SubmittedAt.Equal(b.SubmittedAt):
		return a.SubmittedAt.Compare(b.SubmittedAt)
	}
	return strings.Compare(a.ProposalID, b.ProposalID)
}

// weightedSum uses min-max normalization; a criterion where every candidate
// ties contributes its full weight to everyone.
func weightedSum(rs []Ranked, w [4]float64) {
	lo, hi := bounds(rs)
	for i := range rs {
		v := rs[i].values()
		for k := range 4 {
			n := 1.0
			if hi[k] > lo[k] {
				n = (v[k] - lo[k]) / (hi[k] - lo[k])
				if benefit[k] < 0 {
					n = 1 - n
				}
			}
			rs[i].Contributions[k] = w[k] * n
			rs[i].Score += w[k] * n
		}
	}
}

// topsis ranks by closeness to the ideal point after vector normalization.
func topsis(rs []Ranked, w [4]float64) {
	var norm [4]float64
	for _, r := range rs {
		v := r.values()
		for k := range 4 {
			norm[k] += v[k] * v[k]
		}
	}
	for k := range 4 {
		norm[k] = math.Sqrt(norm[k])
	}
	weighted := make([][4]float64, len(rs))
	ideal, anti := [4]float64{}, [4]float64{}
	for i, r := range rs {
		v := r.values()
		for k := range 4 {
			if norm[k] > 0 {
				weighted[i][k] = w[k] * v[k] / norm[k]
			}
			if i == 0 {
				ideal[k], anti[k] = weighted[i][k], weighted[i][k]
				continue
			}
			best := benefit[k] > 0 && weighted[i][k] > ideal[k] || benefit[k] < 0 && weighted[i][k] < ideal[k]
			worst := benefit[k] > 0 && weighted[i][k] < anti[k] || benefit[k] < 0 && weighted[i][k] > anti[k]
			if best {
				ideal[k] = weighted[i][k]
			}
			if worst {
				anti[k] = weighted[i][k]
			}
		}
	}
	for i := range rs {
		var dPlus, dMinus float64
		for k := range 4 {
			dPlus += (weighted[i][k] - ideal[k]) * (weighted[i][k] - ideal[k])
			dMinus += (weighted[i][k] - anti[k]) * (weighted[i][k] - anti[k])
		}
		dPlus, dMinus = math.Sqrt(dPlus), math.Sqrt(dMinus)
		if dPlus+dMinus == 0 {
			rs[i].Score = 1 // every candidate identical: all ideal
			continue
		}
		rs[i].Score = dMinus / (dPlus + dMinus)
	}
}

func bounds(rs []Ranked) (lo, hi [4]float64) {
	for i, r := range rs {
		v := r.values()
		for k := range 4 {
			if i == 0 || v[k] < lo[k] {
				lo[k] = v[k]
			}
			if i == 0 || v[k] > hi[k] {
				hi[k] = v[k]
			}
		}
	}
	return lo, hi
}

// dominates reports whether a is at least as good as b on every criterion and
// strictly better on one.
func dominates(a, b Candidate) bool {
	va, vb := a.values(), b.values()
	strictly := false
	for k := range 4 {
		da, db := benefit[k]*va[k], benefit[k]*vb[k]
		if da < db {
			return false
		}
		if da > db {
			strictly = true
		}
	}
	return strictly
}
