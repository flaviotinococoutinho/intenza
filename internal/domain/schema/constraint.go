package schema

import (
	"errors"
	"slices"
	"strings"
)

// Op is the kind of predicate a buyer puts on a field.
type Op string

const (
	Eq    Op = "eq"
	In    Op = "in"
	Range Op = "range"
)

// Constraint is a predicate over one field.
type Constraint struct {
	Field    string
	Op       Op
	Values   []Value // Eq (one) and In
	Min, Max *int64  // Range, inclusive
	Weight   int     // 1..5 importance in adherence
	Required bool    // a hard constraint: failing it marks the proposal non-compliant
}

// Constraints are the predicates of an intent, ordered by field.
type Constraints []Constraint

// ParseConstraints reads the buyer's predicates. A bare value means equality;
// an object may carry "in", "min", "max", "weight" and "required":
//
//	{"language": "pt", "grade": {"min": 9, "required": true}, "condition": {"in": ["NM", "LP"]}}
func (s Schema) ParseConstraints(raw map[string]any) (Constraints, error) {
	var out Constraints
	var errs Errors
	for _, k := range sortedKeys(raw) {
		f, ok := s.Field(k)
		if !ok {
			errs = append(errs, FieldError{k, "atributo não existe no schema da categoria"})
			continue
		}
		c, err := f.parseConstraint(raw[k])
		if err != nil {
			errs = append(errs, FieldError{k, err.Error()})
			continue
		}
		out = append(out, c)
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return out, nil
}

func (f Field) parseConstraint(raw any) (Constraint, error) {
	c := Constraint{Field: f.Key, Weight: 1}
	obj, isObj := raw.(map[string]any)
	if !isObj {
		v, err := f.ParseValue(raw)
		if err != nil {
			return c, err
		}
		c.Op, c.Values = Eq, []Value{v}
		return c, nil
	}
	for k := range obj {
		switch k {
		case "in", "min", "max", "weight", "required", "eq":
		default:
			return c, errors.New("chave desconhecida " + k)
		}
	}
	if w, ok := obj["weight"].(float64); ok {
		if w < 1 || w > 5 || w != float64(int(w)) {
			return c, errors.New("peso vai de 1 a 5")
		}
		c.Weight = int(w)
	}
	if r, ok := obj["required"].(bool); ok {
		c.Required = r
	}
	switch {
	case obj["in"] != nil:
		list, ok := obj["in"].([]any)
		if !ok || len(list) == 0 {
			return c, errors.New("in espera uma lista não vazia")
		}
		for _, item := range list {
			v, err := f.ParseValue(item)
			if err != nil {
				return c, err
			}
			c.Values = append(c.Values, v)
		}
		c.Op = In
	case obj["eq"] != nil:
		v, err := f.ParseValue(obj["eq"])
		if err != nil {
			return c, err
		}
		c.Op, c.Values = Eq, []Value{v}
	case obj["min"] != nil || obj["max"] != nil:
		if !f.Kind.numeric() {
			return c, errors.New("faixa só vale para campos numéricos")
		}
		for key, dst := range map[string]**int64{"min": &c.Min, "max": &c.Max} {
			if obj[key] == nil {
				continue
			}
			v, err := f.ParseValue(obj[key])
			if err != nil {
				return c, err
			}
			n := v.Num
			*dst = &n
		}
		if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
			return c, errors.New("mínimo maior que máximo")
		}
		c.Op = Range
	default:
		return c, errors.New("restrição vazia")
	}
	return c, nil
}

// Satisfied evaluates the predicate on a fact; a missing fact never satisfies.
func (c Constraint) Satisfied(f Facts) bool {
	v, ok := f[c.Field]
	if !ok {
		return false
	}
	switch c.Op {
	case Eq, In:
		return slices.ContainsFunc(c.Values, func(x Value) bool { return Equal(x, v) })
	case Range:
		return (c.Min == nil || v.Num >= *c.Min) && (c.Max == nil || v.Num <= *c.Max)
	}
	return false
}

// Match is the outcome of one predicate, kept for explanation.
type Match struct {
	Field     string `json:"field"`
	Satisfied bool   `json:"satisfied"`
	Weight    int    `json:"weight"`
	Required  bool   `json:"required"`
}

// Adherence is the weighted share of satisfied predicates in [0, 1] and
// whether every hard predicate holds. No predicate means full adherence.
func Adherence(cs Constraints, f Facts) (score float64, compliant bool, detail []Match) {
	compliant = true
	var got, total int
	for _, c := range cs {
		ok := c.Satisfied(f)
		detail = append(detail, Match{Field: c.Field, Satisfied: ok, Weight: c.Weight, Required: c.Required})
		total += c.Weight
		if ok {
			got += c.Weight
		} else if c.Required {
			compliant = false
		}
	}
	if total == 0 {
		return 1, compliant, detail
	}
	return float64(got) / float64(total), compliant, detail
}

// Discrepancy is a field whose observed value differs from the promised one.
type Discrepancy struct {
	Field    string `json:"field"`
	Promised string `json:"promised"`
	Observed string `json:"observed"`
}

// Conference compares, for each claimed evidentiary field, what the terms
// promised with what was observed on delivery. A claim on a field that is not
// evidentiary, or that the terms did not cover, is reported as a problem: a
// dispute can only be about what the contract actually fixed.
func (s Schema) Conference(promised, observed Facts, claimed []string) ([]Discrepancy, error) {
	var out []Discrepancy
	var errs Errors
	for _, key := range claimed {
		f, ok := s.Field(key)
		switch {
		case !ok:
			errs = append(errs, FieldError{key, "atributo não existe no schema"})
			continue
		case !f.Evidentiary:
			errs = append(errs, FieldError{key, "atributo não é probatório"})
			continue
		}
		p, okP := promised[key]
		o, okO := observed[key]
		if !okP {
			errs = append(errs, FieldError{key, "o termo não fixou esse atributo"})
			continue
		}
		if !okO {
			errs = append(errs, FieldError{key, "falta o valor observado"})
			continue
		}
		if !Equal(p, o) {
			out = append(out, Discrepancy{Field: key, Promised: p.Format(f), Observed: o.Format(f)})
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return out, nil
}

// Canonical renders facts as a map of formatted strings, the shape frozen in
// the terms snapshot.
func (s Schema) Canonical(f Facts) map[string]string {
	out := make(map[string]string, len(f))
	for k, v := range f {
		if fd, ok := s.Field(k); ok {
			out[k] = v.Format(fd)
		}
	}
	return out
}

// Describe renders a constraint for people ("grade ≥ 9.0").
func (s Schema) Describe(c Constraint) string {
	f, _ := s.Field(c.Field)
	switch c.Op {
	case Eq:
		return c.Field + " = " + c.Values[0].Format(f)
	case In:
		vals := make([]string, len(c.Values))
		for i, v := range c.Values {
			vals[i] = v.Format(f)
		}
		return c.Field + " ∈ {" + strings.Join(vals, ", ") + "}"
	case Range:
		lo, hi := "−∞", "+∞"
		if c.Min != nil {
			lo = Value{Kind: f.Kind, Num: *c.Min}.Format(f)
		}
		if c.Max != nil {
			hi = Value{Kind: f.Kind, Num: *c.Max}.Format(f)
		}
		return c.Field + " ∈ [" + lo + ", " + hi + "]"
	}
	return c.Field
}
