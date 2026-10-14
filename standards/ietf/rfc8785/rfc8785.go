//intenza:paradigm functional-core

// Package rfc8785 implements the JSON Canonicalization Scheme (JCS): the same
// data always serializes to the same bytes, so a hash of it identifies the
// data and not its formatting.
//
// Three rules carry the scheme: numbers as ECMAScript prints them (shortest
// round-trip digits, fixed notation between 1e-7 and 1e21), strings with only
// the mandatory escapes, and object members sorted by UTF-16 code units, which
// differs from byte order for characters outside the BMP. Input must be I-JSON:
// duplicate member names are rejected instead of silently keeping the last.
package rfc8785

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

var (
	ErrDuplicate = errors.New("rfc8785: duplicate member name")
	ErrNumber    = errors.New("rfc8785: number not representable as IEEE 754 double")
)

// Transform canonicalizes JSON text.
func Transform(in []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(in))
	dec.UseNumber()
	v, err := decode(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("rfc8785: trailing data after JSON value")
	}
	out := make([]byte, 0, len(in))
	return encode(out, v)
}

// Marshal encodes v with encoding/json and canonicalizes the result.
func Marshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Transform(raw)
}

type member struct {
	key string
	val any
}

// decode builds a tree from the token stream so duplicates can be caught,
// which json.Unmarshal into a map cannot do.
func decode(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			var obj []member
			seen := map[string]bool{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k := kt.(string)
				if seen[k] {
					return nil, fmt.Errorf("%w: %q", ErrDuplicate, k)
				}
				seen[k] = true
				v, err := decode(dec)
				if err != nil {
					return nil, err
				}
				obj = append(obj, member{k, v})
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			slices.SortFunc(obj, func(a, b member) int { return compareUTF16(a.key, b.key) })
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := decode(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil || math.IsInf(f, 0) {
			return nil, ErrNumber
		}
		return f, nil
	case string, bool, nil:
		return t, nil
	}
	return nil, fmt.Errorf("rfc8785: unexpected token %v", tok)
}

func encode(b []byte, v any) ([]byte, error) {
	switch t := v.(type) {
	case nil:
		return append(b, "null"...), nil
	case bool:
		if t {
			return append(b, "true"...), nil
		}
		return append(b, "false"...), nil
	case float64:
		return AppendNumber(b, t)
	case string:
		return AppendString(b, t), nil
	case []any:
		b = append(b, '[')
		for i, e := range t {
			if i > 0 {
				b = append(b, ',')
			}
			var err error
			if b, err = encode(b, e); err != nil {
				return nil, err
			}
		}
		return append(b, ']'), nil
	case []member:
		b = append(b, '{')
		for i, m := range t {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(AppendString(b, m.key), ':')
			var err error
			if b, err = encode(b, m.val); err != nil {
				return nil, err
			}
		}
		return append(b, '}'), nil
	}
	return nil, fmt.Errorf("rfc8785: unsupported value %T", v)
}

// AppendNumber appends f as ECMAScript's Number.prototype.toString prints it.
func AppendNumber(b []byte, f float64) ([]byte, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, ErrNumber
	}
	if f == 0 {
		return append(b, '0'), nil // also -0
	}
	if f < 0 {
		b = append(b, '-')
		f = -f
	}
	var scratch [32]byte
	e := strconv.AppendFloat(scratch[:0], f, 'e', -1, 64) // d.ddddde±XX, shortest round-trip
	mant, exp := e, 0
	if i := bytes.IndexByte(e, 'e'); i >= 0 {
		mant = e[:i]
		x, _ := strconv.Atoi(string(e[i+1:]))
		exp = x
	}
	var digits [24]byte
	k := 0
	for _, c := range mant {
		if c != '.' {
			digits[k] = c
			k++
		}
	}
	n := exp + 1 // position of the decimal point relative to the digits
	switch {
	case k <= n && n <= 21:
		b = append(b, digits[:k]...)
		for range n - k {
			b = append(b, '0')
		}
	case 0 < n && n <= 21:
		b = append(append(append(b, digits[:n]...), '.'), digits[n:k]...)
	case -6 < n && n <= 0:
		b = append(b, '0', '.')
		for range -n {
			b = append(b, '0')
		}
		b = append(b, digits[:k]...)
	default:
		b = append(b, digits[0])
		if k > 1 {
			b = append(append(b, '.'), digits[1:k]...)
		}
		b = append(b, 'e')
		if n-1 >= 0 {
			b = append(b, '+')
		}
		b = strconv.AppendInt(b, int64(n-1), 10)
	}
	return b, nil
}

const hexDigits = "0123456789abcdef"

// AppendString appends s as a JSON string with only the escapes JCS requires.
func AppendString(b []byte, s string) []byte {
	b = append(b, '"')
	for i := range len(s) {
		c := s[i]
		switch {
		case c == '"':
			b = append(b, '\\', '"')
		case c == '\\':
			b = append(b, '\\', '\\')
		case c >= 0x20:
			b = append(b, c)
		case c == '\b':
			b = append(b, '\\', 'b')
		case c == '\t':
			b = append(b, '\\', 't')
		case c == '\n':
			b = append(b, '\\', 'n')
		case c == '\f':
			b = append(b, '\\', 'f')
		case c == '\r':
			b = append(b, '\\', 'r')
		default:
			b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
		}
	}
	return append(b, '"')
}

// compareUTF16 orders strings by UTF-16 code units without converting them:
// runes up to U+FFFF compare as themselves, larger ones as surrogate pairs.
func compareUTF16(a, b string) int {
	for a != "" && b != "" {
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)
		if ra != rb {
			ua, ub := firstUnit(ra), firstUnit(rb)
			if ua != ub {
				return int(ua) - int(ub)
			}
			return int(secondUnit(ra)) - int(secondUnit(rb))
		}
		a, b = a[na:], b[nb:]
	}
	return len(a) - len(b)
}

func firstUnit(r rune) uint16 {
	if r < 0x10000 {
		return uint16(r)
	}
	return uint16(0xD800 + ((r - 0x10000) >> 10))
}

func secondUnit(r rune) uint16 {
	if r < 0x10000 {
		return 0
	}
	return uint16(0xDC00 + ((r - 0x10000) & 0x3FF))
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "ietf-rfc-8785",
		Code:      "RFC 8785",
		Title:     "JSON Canonicalization Scheme (JCS)",
		Authority: meta.IETF,
		Kind:      meta.Grammar,
		Edition:   "RFC 8785 (junho de 2020)",
		Package:   "ietf/rfc8785",
		Structure: "árvore a partir do fluxo de tokens (detecta nomes duplicados); números no algoritmo do ECMAScript sobre os dígitos mais curtos do strconv; ordem por unidades UTF-16 sem conversão",
		Verification: "todas as amostras numéricas do apêndice B; exemplo de ordenação UTF-16 e bytes UTF-8 da seção 3.2.4; " +
			"idempotência por fuzz",
		Sources: []meta.Source{
			{
				Name: "RFC 8785", URL: "https://www.rfc-editor.org/rfc/rfc8785", Retrieved: "2026-10-14",
				License: "IETF Trust", Grounding: meta.Algorithmic,
			},
		},
	}
}
