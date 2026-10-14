//intenza:paradigm functional-core

// Package schema is the typed attribute engine: the evidentiary spine of every
// contract. A buyer writes constraints (predicates over the attribute space),
// a seller declares facts (a point in that space), adherence measures how many
// predicates the point satisfies, and a dispute compares promised facts with
// observed ones field by field.
//
// Attribute kinds backed by a norm (country, language, GTIN, ISBN, VIN,
// RENAVAM) validate through the standards SDKs, so a mistyped identifier never
// becomes evidence.
package schema

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/flaviotinococoutinho/intenza/standards/gs1/gtin"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso2108"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso3166"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso3779"
	"github.com/flaviotinococoutinho/intenza/standards/iso/iso639"
	"github.com/flaviotinococoutinho/intenza/standards/senatran/renavam"
)

// Kind is the quality structure a field takes values in.
type Kind string

const (
	Text     Kind = "text"
	Enum     Kind = "enum"
	Int      Kind = "int"
	Decimal  Kind = "decimal" // fixed point with Scale digits
	Bool     Kind = "bool"
	Year     Kind = "year"
	Country  Kind = "country"  // ISO 3166-1 alpha-2
	Language Kind = "language" // ISO 639
	GTIN     Kind = "gtin"
	ISBN     Kind = "isbn"
	VIN      Kind = "vin"
	RENAVAM  Kind = "renavam"
)

// numeric kinds compare by Num; the rest by Str (or Bool).
func (k Kind) numeric() bool { return k == Int || k == Decimal || k == Year }

func (k Kind) known() bool {
	switch k {
	case Text, Enum, Int, Decimal, Bool, Year, Country, Language, GTIN, ISBN, VIN, RENAVAM:
		return true
	}
	return false
}

// Condition makes a field required depending on another field's value.
type Condition struct {
	Field string   `yaml:"field"`
	In    []string `yaml:"in,omitempty"`
	NotIn []string `yaml:"not_in,omitempty"`
}

func (c Condition) holds(f Facts) bool {
	v, ok := f[c.Field]
	if !ok {
		return false
	}
	if len(c.In) > 0 {
		return slices.Contains(c.In, v.Str)
	}
	return !slices.Contains(c.NotIn, v.Str)
}

// Field is one typed attribute of a category.
type Field struct {
	Key         string     `yaml:"key"`
	Label       string     `yaml:"label"`
	Kind        Kind       `yaml:"kind"`
	Required    bool       `yaml:"required,omitempty"`
	RequiredIf  *Condition `yaml:"required_if,omitempty"`
	Evidentiary bool       `yaml:"evidentiary,omitempty"`
	Values      []string   `yaml:"values,omitempty"`
	Min         string     `yaml:"min,omitempty"`
	Max         string     `yaml:"max,omitempty"`
	Step        string     `yaml:"step,omitempty"`
	Scale       int        `yaml:"scale,omitempty"`
	MaxLen      int        `yaml:"max_len,omitempty"`
	Unit        string     `yaml:"unit,omitempty"`

	min, max, step *int64
}

// Schema is the flattened, versioned set of fields of a leaf category.
type Schema struct {
	Category string
	Version  int
	Fields   []Field
	index    map[string]int
}

// New validates the fields and indexes them. A schema that fails here never
// reaches users: the taxonomy is built at start-up and fails fast.
func New(category string, version int, fields []Field) (Schema, error) {
	s := Schema{Category: category, Version: version, index: make(map[string]int, len(fields))}
	for i, f := range fields {
		if f.Key == "" || f.Label == "" || !f.Kind.known() {
			return Schema{}, fmt.Errorf("schema %s: field %q incompleto ou de tipo desconhecido %q", category, f.Key, f.Kind)
		}
		if _, dup := s.index[f.Key]; dup {
			return Schema{}, fmt.Errorf("schema %s: campo %q duplicado", category, f.Key)
		}
		if f.Kind == Enum && len(f.Values) == 0 {
			return Schema{}, fmt.Errorf("schema %s: enum %q sem valores", category, f.Key)
		}
		var err error
		if f.min, err = f.bound(f.Min); err != nil {
			return Schema{}, err
		}
		if f.max, err = f.bound(f.Max); err != nil {
			return Schema{}, err
		}
		if f.step, err = f.bound(f.Step); err != nil {
			return Schema{}, err
		}
		s.index[f.Key] = i
		s.Fields = append(s.Fields, f)
	}
	for _, f := range s.Fields {
		if f.RequiredIf != nil {
			if _, ok := s.index[f.RequiredIf.Field]; !ok {
				return Schema{}, fmt.Errorf("schema %s: %q depende de campo inexistente %q", category, f.Key, f.RequiredIf.Field)
			}
		}
	}
	return s, nil
}

func (f Field) bound(s string) (*int64, error) {
	if s == "" {
		return nil, nil
	}
	v, err := f.parseNumber(s)
	if err != nil {
		return nil, fmt.Errorf("campo %s: limite %q: %w", f.Key, s, err)
	}
	return &v, nil
}

// parseNumber reads an integer or a fixed-point decimal scaled by f.Scale.
func (f Field) parseNumber(s string) (int64, error) {
	if f.Kind != Decimal {
		return strconv.ParseInt(s, 10, 64)
	}
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > f.Scale {
		return 0, fmt.Errorf("mais de %d casas decimais", f.Scale)
	}
	frac += strings.Repeat("0", f.Scale-len(frac))
	v, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// Field returns the field with the key.
func (s Schema) Field(key string) (Field, bool) {
	i, ok := s.index[key]
	if !ok {
		return Field{}, false
	}
	return s.Fields[i], true
}

// Evidentiary lists the keys conferred in a dispute.
func (s Schema) Evidentiary() []string {
	var out []string
	for _, f := range s.Fields {
		if f.Evidentiary {
			out = append(out, f.Key)
		}
	}
	return out
}

// Value is a typed attribute value.
type Value struct {
	Kind Kind
	Str  string
	Num  int64
	Bool bool
}

// Format renders the value for people and for the canonical terms.
func (v Value) Format(f Field) string {
	switch {
	case v.Kind == Bool:
		return strconv.FormatBool(v.Bool)
	case v.Kind == Decimal:
		neg := v.Num < 0
		n := v.Num
		if neg {
			n = -n
		}
		s := strconv.FormatInt(n, 10)
		for len(s) <= f.Scale {
			s = "0" + s
		}
		if f.Scale > 0 {
			s = s[:len(s)-f.Scale] + "." + s[len(s)-f.Scale:]
		}
		if neg {
			s = "-" + s
		}
		return s
	case v.Kind.numeric():
		return strconv.FormatInt(v.Num, 10)
	}
	return v.Str
}

// Equal compares two values of the same field.
func Equal(a, b Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch {
	case a.Kind == Bool:
		return a.Bool == b.Bool
	case a.Kind.numeric():
		return a.Num == b.Num
	}
	return a.Str == b.Str
}

// Facts are the attribute values a seller declares about the item.
type Facts map[string]Value

// FieldError is one problem with one field, ready for an RFC 9457 errors list.
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Errors collects every field problem instead of stopping at the first.
type Errors []FieldError

func (e Errors) Error() string {
	parts := make([]string, len(e))
	for i, fe := range e {
		parts[i] = fe.Field + ": " + fe.Reason
	}
	return "atributos inválidos: " + strings.Join(parts, "; ")
}

// ErrInvalid is wrapped by every validation failure.
var ErrInvalid = errors.New("schema: invalid attributes")

func (e Errors) Unwrap() error { return ErrInvalid }

// ParseValue reads a raw JSON value (string, float64 or bool) into a typed value.
func (f Field) ParseValue(raw any) (Value, error) {
	v := Value{Kind: f.Kind}
	switch f.Kind {
	case Bool:
		b, ok := raw.(bool)
		if !ok {
			return v, errors.New("esperado verdadeiro ou falso")
		}
		v.Bool = b
		return v, nil
	case Int, Year, Decimal:
		var s string
		switch t := raw.(type) {
		case float64:
			s = strconv.FormatFloat(t, 'f', -1, 64)
		case string:
			s = t
		default:
			return v, errors.New("esperado número")
		}
		n, err := f.parseNumber(s)
		if err != nil {
			return v, errors.New("número inválido: " + err.Error())
		}
		v.Num = n
		return v, f.checkNumber(n)
	case Text, Enum, Country, Language, GTIN, ISBN, VIN, RENAVAM:
	}
	s, ok := raw.(string)
	if !ok {
		return v, errors.New("esperado texto")
	}
	norm, err := f.normalizeText(strings.TrimSpace(s))
	if err != nil {
		return v, err
	}
	v.Str = norm
	return v, nil
}

func (f Field) checkNumber(n int64) error {
	if f.min != nil && n < *f.min {
		return fmt.Errorf("abaixo do mínimo %s", f.Min)
	}
	if f.max != nil && n > *f.max {
		return fmt.Errorf("acima do máximo %s", f.Max)
	}
	if f.step != nil && *f.step > 0 {
		base := int64(0)
		if f.min != nil {
			base = *f.min
		}
		if (n-base)%*f.step != 0 {
			return fmt.Errorf("fora do passo %s", f.Step)
		}
	}
	return nil
}

// normalizeText validates text-like kinds, delegating normative ones to the SDKs.
func (f Field) normalizeText(s string) (string, error) {
	switch f.Kind {
	case Text:
		if s == "" {
			return "", errors.New("texto vazio")
		}
		if f.MaxLen > 0 && len([]rune(s)) > f.MaxLen {
			return "", fmt.Errorf("mais de %d caracteres", f.MaxLen)
		}
		return s, nil
	case Enum:
		if !slices.Contains(f.Values, s) {
			return "", fmt.Errorf("valor fora de %v", f.Values)
		}
		return s, nil
	case Country:
		c, ok := iso3166.ByAlpha2(s)
		if !ok {
			return "", errors.New("país desconhecido na ISO 3166-1")
		}
		return c.Alpha2(), nil
	case Language:
		l, err := iso639.Parse(strings.ToLower(s))
		if err != nil {
			return "", errors.New("idioma desconhecido na ISO 639")
		}
		return l.String(), nil
	case GTIN:
		n, err := gtin.Normalize(s)
		if err != nil {
			return "", errors.New("GTIN inválido: " + err.Error())
		}
		return n, nil
	case ISBN:
		n, err := iso2108.To13(s)
		if err != nil {
			return "", errors.New("ISBN inválido: " + err.Error())
		}
		return n, nil
	case VIN:
		u := strings.ToUpper(s)
		if !iso3779.Valid(u) {
			return "", errors.New("VIN fora da sintaxe ISO 3779")
		}
		return u, nil
	case RENAVAM:
		n, err := renavam.Normalize(s)
		if err != nil {
			return "", errors.New("RENAVAM inválido: " + err.Error())
		}
		return n, nil
	case Int, Decimal, Bool, Year:
	}
	return "", fmt.Errorf("tipo %s não é textual", f.Kind)
}

// ParseFacts validates a raw attribute map against the schema: unknown keys,
// missing required fields and bad values are all reported together.
func (s Schema) ParseFacts(raw map[string]any) (Facts, error) {
	facts := make(Facts, len(raw))
	var errs Errors
	for _, k := range sortedKeys(raw) {
		f, ok := s.Field(k)
		if !ok {
			errs = append(errs, FieldError{k, "atributo não existe no schema da categoria"})
			continue
		}
		v, err := f.ParseValue(raw[k])
		if err != nil {
			errs = append(errs, FieldError{k, err.Error()})
			continue
		}
		facts[k] = v
	}
	for _, f := range s.Fields {
		if _, ok := facts[f.Key]; ok {
			continue
		}
		if f.Required || (f.RequiredIf != nil && f.RequiredIf.holds(facts)) {
			errs = append(errs, FieldError{f.Key, "obrigatório"})
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return facts, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
