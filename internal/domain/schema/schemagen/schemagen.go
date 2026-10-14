//intenza:paradigm probabilistic

// Package schemagen draws random but valid attribute values for any schema.
// Property tests use it to explore the attribute space and the load generator
// uses it to fake a market; both pass their own seeded source, so a failing
// case can be replayed.
package schemagen

import (
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/flaviotinococoutinho/intenza/internal/domain/schema"
	"github.com/flaviotinococoutinho/intenza/standards/gs1/gtin"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso2108"
	"github.com/flaviotinococoutinho/intenza/standards/senatran/renavam"
)

var (
	countries = []string{"BR", "US", "JP", "PT", "FR", "DE", "GB", "IT", "AR"}
	languages = []string{"pt", "en", "ja", "fr", "de", "es", "it"}
	words     = []string{"aurora", "basalto", "cobalto", "duna", "eclipse", "fóssil", "granito", "horizonte", "índigo", "jade"}
)

const vinAlphabet = "0123456789ABCDEFGHJKLMNPRSTUVWXYZ"

// Facts returns raw values (as decoded from JSON) for every required field and
// for each optional field with probability optional.
func Facts(r *rand.Rand, s schema.Schema, optional float64) map[string]any {
	raw := map[string]any{}
	for _, f := range s.Fields {
		if f.RequiredIf != nil {
			continue
		}
		if f.Required || r.Float64() < optional {
			raw[f.Key] = Value(r, f)
		}
	}
	for _, f := range s.Fields {
		c := f.RequiredIf
		if c == nil {
			continue
		}
		v, _ := raw[c.Field].(string)
		_, present := raw[c.Field]
		holds := present && ((len(c.In) > 0 && contains(c.In, v)) || (len(c.In) == 0 && !contains(c.NotIn, v)))
		if holds || r.Float64() < optional/2 {
			raw[f.Key] = Value(r, f)
		}
	}
	return raw
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// Value draws one valid raw value for the field.
func Value(r *rand.Rand, f schema.Field) any {
	switch f.Kind {
	case schema.Bool:
		return r.IntN(2) == 1
	case schema.Enum:
		return f.Values[r.IntN(len(f.Values))]
	case schema.Text:
		w := words[r.IntN(len(words))] + " " + strconv.Itoa(r.IntN(1000))
		if f.MaxLen > 0 && len([]rune(w)) > f.MaxLen {
			w = string([]rune(w)[:f.MaxLen])
		}
		return strings.TrimSpace(w)
	case schema.Country:
		return countries[r.IntN(len(countries))]
	case schema.Language:
		return languages[r.IntN(len(languages))]
	case schema.GTIN:
		body := digits(r, 12)
		d, _ := gtin.CheckDigit(body)
		return body + string(d)
	case schema.ISBN:
		body := "978" + digits(r, 9)
		d, _ := gtin.CheckDigit(body)
		n, _ := iso2108.Normalize(body + string(d))
		return n
	case schema.VIN:
		var b [17]byte
		for i := range b {
			b[i] = vinAlphabet[r.IntN(len(vinAlphabet))]
		}
		return string(b[:])
	case schema.RENAVAM:
		body := digits(r, 10)
		d, _ := renavam.CheckDigit(body)
		return body + string(d)
	case schema.Int, schema.Year, schema.Decimal:
		return number(r, f)
	}
	return nil
}

func digits(r *rand.Rand, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('0' + r.IntN(10))
	}
	return string(b)
}

// number draws inside [min, max] on the step grid and returns it as JSON would.
func number(r *rand.Rand, f schema.Field) any {
	lo, hi, step := bound(f, f.Min, 0), bound(f, f.Max, 1000), bound(f, f.Step, 1)
	if f.Kind == schema.Decimal && f.Step == "" {
		step = 1
	}
	if step <= 0 {
		step = 1
	}
	n := lo + step*r.Int64N((hi-lo)/step+1)
	if f.Kind != schema.Decimal {
		return float64(n)
	}
	v := schema.Value{Kind: schema.Decimal, Num: n}
	return v.Format(f)
}

func bound(f schema.Field, s string, def int64) int64 {
	if s == "" {
		return def
	}
	whole, frac, _ := strings.Cut(s, ".")
	for len(frac) < f.Scale {
		frac += "0"
	}
	if f.Kind != schema.Decimal {
		frac = ""
	}
	n, _ := strconv.ParseInt(whole+frac, 10, 64)
	return n
}

// Constraints derives buyer predicates from a reference item: each chosen
// field becomes an equality (or a range around numeric values); with
// probability miss the predicate is pushed away so the item fails it.
func Constraints(r *rand.Rand, s schema.Schema, item map[string]any, pick, miss float64) map[string]any {
	out := map[string]any{}
	for _, f := range s.Fields {
		v, ok := item[f.Key]
		if !ok || r.Float64() >= pick {
			continue
		}
		c := map[string]any{"weight": float64(1 + r.IntN(5)), "required": r.IntN(4) == 0}
		switch x := v.(type) {
		case float64:
			fd, _ := s.Field(f.Key)
			lo, hi := bound(fd, fd.Min, -1<<40), bound(fd, fd.Max, 1<<40)
			n := int64(x)
			cmin, cmax := n, min(n+int64(r.IntN(3)), hi)
			if r.Float64() < miss {
				switch {
				case n+1 <= hi:
					cmin, cmax = n+1, hi
				case n-1 >= lo:
					cmin, cmax = lo, n-1
				}
			}
			c["min"], c["max"] = float64(cmin), float64(cmax)
		default:
			c["eq"] = v
			if r.Float64() < miss {
				alt := Value(r, f)
				if alt == v {
					continue
				}
				c["eq"] = alt
			}
		}
		out[f.Key] = c
	}
	return out
}
