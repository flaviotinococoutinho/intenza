//intenza:paradigm probabilistic

// Package reputation estimates how reliable a party is in a role from the
// ratings it received, and says how sure the estimate is.
//
// A rating of 4 or 5 stars counts as a success. Successes and failures update
// a Beta prior (pseudo-counts, so a single 5 cannot beat a long 4.8 history),
// older ratings weigh less (exponential half-life), and the 95% credible
// interval comes from inverting the regularized incomplete beta function.
// Ratings are revealed double-blind, and the notification fan-out samples
// from the posterior (Thompson sampling) so sellers without history still get
// seen. Randomness is always a parameter.
package reputation

import (
	"errors"
	"math"
	"math/rand/v2"
	"slices"
	"time"
)

// Role is the role the rated party played.
type Role string

const (
	AsSeller Role = "seller"
	AsBuyer  Role = "buyer"
)

// Event types.
const (
	EvRatingSubmitted = "intenza.reputation.rating-submitted.v1"
	EvRatingsRevealed = "intenza.reputation.ratings-revealed.v1"
)

var (
	ErrStars   = errors.New("reputation: stars from 1 to 5")
	ErrTwice   = errors.New("reputation: this party already rated")
	ErrWindow  = errors.New("reputation: rating window is over")
	ErrNotPair = errors.New("reputation: only the two parties rate each other")
)

// Rating is one party's judgement of the other after a transaction.
type Rating struct {
	AgreementID string    `json:"agreement_id"`
	RaterID     string    `json:"rater_id"`
	RateeID     string    `json:"ratee_id"`
	Role        Role      `json:"role"` // role of the ratee
	Stars       int       `json:"stars"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// Prior is a Beta(Alpha, Beta) prior expressed as pseudo-counts.
type Prior struct{ Alpha, Beta float64 }

// DefaultPrior is the platform-wide belief before any rating: mean 0.8,
// worth five ratings.
var DefaultPrior = Prior{Alpha: 4, Beta: 1}

// DefaultHalfLife makes a rating from a year ago count half.
const DefaultHalfLife = 365 * 24 * time.Hour

// Score is the posterior with its uncertainty.
type Score struct {
	N     int     `json:"n"`
	Alpha float64 `json:"alpha"`
	Beta  float64 `json:"beta"`
	Mean  float64 `json:"mean"`
	Low   float64 `json:"low"`  // 2.5% quantile
	High  float64 `json:"high"` // 97.5% quantile
}

// Estimate computes the posterior of the ratings at instant now.
func Estimate(ratings []Rating, prior Prior, halfLife time.Duration, now time.Time) Score {
	a, b := prior.Alpha, prior.Beta
	for _, r := range ratings {
		w := 1.0
		if age := now.Sub(r.SubmittedAt); age > 0 && halfLife > 0 {
			w = math.Exp2(-float64(age) / float64(halfLife))
		}
		if r.Stars >= 4 {
			a += w
		} else {
			b += w
		}
	}
	return Score{N: len(ratings), Alpha: a, Beta: b, Mean: a / (a + b), Low: BetaQuantile(0.025, a, b), High: BetaQuantile(0.975, a, b)}
}

// Sample draws one plausible reliability from the posterior.
func (s Score) Sample(r *rand.Rand) float64 {
	x := gamma(r, s.Alpha)
	y := gamma(r, s.Beta)
	return x / (x + y)
}

// gamma draws from Gamma(k, 1) by Marsaglia and Tsang (2000).
func gamma(r *rand.Rand, k float64) float64 {
	if k < 1 {
		return gamma(r, k+1) * math.Pow(r.Float64(), 1/k)
	}
	d := k - 1.0/3
	c := 1 / math.Sqrt(9*d)
	for {
		x := r.NormFloat64()
		v := 1 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := r.Float64()
		if u < 1-0.0331*x*x*x*x || math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}

// RegIncBeta is the regularized incomplete beta function I_x(a, b),
// by Lentz's continued fraction.
func RegIncBeta(x, a, b float64) float64 {
	switch {
	case x <= 0:
		return 0
	case x >= 1:
		return 1
	}
	la, _ := math.Lgamma(a + b)
	lb, _ := math.Lgamma(a)
	lc, _ := math.Lgamma(b)
	front := math.Exp(la - lb - lc + a*math.Log(x) + b*math.Log(1-x))
	if x < (a+1)/(a+b+2) {
		return front * betaCF(x, a, b) / a
	}
	return 1 - front*betaCF(1-x, b, a)/b
}

func betaCF(x, a, b float64) float64 {
	const eps, tiny = 1e-15, 1e-300
	qab, qap, qam := a+b, a+1, a-1
	c, d := 1.0, 1-qab*x/qap
	if math.Abs(d) < tiny {
		d = tiny
	}
	d = 1 / d
	h := d
	for m := 1; m <= 300; m++ {
		fm := float64(m)
		aa := fm * (b - fm) * x / ((qam + 2*fm) * (a + 2*fm))
		d = 1 + aa*d
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = 1 + aa/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1 / d
		h *= d * c
		aa = -(a + fm) * (qab + fm) * x / ((a + 2*fm) * (qap + 2*fm))
		d = 1 + aa*d
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = 1 + aa/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1 / d
		del := d * c
		h *= del
		if math.Abs(del-1) < eps {
			break
		}
	}
	return h
}

// BetaQuantile inverts I_x(a, b) by bisection: 60 halvings reach float precision.
func BetaQuantile(p, a, b float64) float64 {
	lo, hi := 0.0, 1.0
	for range 60 {
		mid := (lo + hi) / 2
		if RegIncBeta(mid, a, b) < p {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// Pair holds the two ratings of an agreement until they can be shown.
type Pair struct {
	AgreementID string    `json:"agreement_id"`
	BuyerID     string    `json:"buyer_id"`
	SellerID    string    `json:"seller_id"`
	WindowEnds  time.Time `json:"window_ends"`
	FromBuyer   *Rating   `json:"from_buyer,omitempty"`
	FromSeller  *Rating   `json:"from_seller,omitempty"`
}

// Submit stores one party's rating, sealed until Revealed.
func (p Pair) Submit(raterID string, stars int, now time.Time) (Pair, error) {
	if stars < 1 || stars > 5 {
		return p, ErrStars
	}
	if !now.Before(p.WindowEnds) {
		return p, ErrWindow
	}
	switch raterID {
	case p.BuyerID:
		if p.FromBuyer != nil {
			return p, ErrTwice
		}
		p.FromBuyer = &Rating{p.AgreementID, raterID, p.SellerID, AsSeller, stars, now}
	case p.SellerID:
		if p.FromSeller != nil {
			return p, ErrTwice
		}
		p.FromSeller = &Rating{p.AgreementID, raterID, p.BuyerID, AsBuyer, stars, now}
	default:
		return p, ErrNotPair
	}
	return p, nil
}

// Revealed returns the ratings once both parties rated or the window closed;
// before that, nobody sees anything, so nobody rates in retaliation.
func (p Pair) Revealed(now time.Time) ([]Rating, bool) {
	both := p.FromBuyer != nil && p.FromSeller != nil
	if !both && now.Before(p.WindowEnds) {
		return nil, false
	}
	var out []Rating
	for _, r := range []*Rating{p.FromBuyer, p.FromSeller} {
		if r != nil {
			out = append(out, *r)
		}
	}
	return out, true
}

// Contender is a seller who may be notified about a new intent.
type Contender struct {
	SellerID string
	Score    Score
}

// Explore picks k sellers by Thompson sampling: each draws a plausible
// reliability from its posterior and the k highest draws win. A seller without
// history has a wide posterior and wins sometimes; a proven one wins often.
func Explore(r *rand.Rand, contenders []Contender, k int) []string {
	type draw struct {
		id string
		v  float64
	}
	draws := make([]draw, len(contenders))
	for i, c := range contenders {
		draws[i] = draw{c.SellerID, c.Score.Sample(r)}
	}
	slices.SortFunc(draws, func(a, b draw) int {
		switch {
		case a.v > b.v:
			return -1
		case a.v < b.v:
			return 1
		}
		if a.id < b.id {
			return -1
		}
		return 1
	})
	out := make([]string, 0, min(k, len(draws)))
	for _, d := range draws[:min(k, len(draws))] {
		out = append(out, d.id)
	}
	return out
}
