//intenza:paradigm automata

// Package iso8601 parses and formats ISO 8601 durations (PnYnMnDTnHnMnS, PnW)
// with a hand-written deterministic automaton: one pass, no regexp, no
// allocation.
//
// ISO 8601 separates nominal durations (years, months, weeks, days, whose
// length depends on the calendar) from exact ones (hours, minutes, seconds).
// The type keeps that distinction: Exact refuses to turn "P1M" into a fixed
// number of nanoseconds, and AddTo applies the calendar part through the
// calendar. Profile: fractions only on the time part, weeks only alone, as in
// the RFC 3339 ABNF.
package iso8601

import (
	"errors"
	"math/bits"
	"strconv"
	"time"

	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

var (
	ErrSyntax   = errors.New("iso8601: invalid duration")
	ErrRange    = errors.New("iso8601: duration out of range")
	ErrNominal  = errors.New("iso8601: duration has calendar components")
	ErrFraction = errors.New("iso8601: fraction only on the smallest time component")
)

// Duration is a parsed ISO 8601 duration.
type Duration struct {
	Years, Months, Weeks, Days int32         // nominal: calendar-dependent
	Clock                      time.Duration // exact: hours, minutes and seconds
	Negative                   bool
}

// designator order: each component must come after the previous one.
const (
	stYear = iota + 1
	stMonth
	stWeek
	stDay
	stHour
	stMinute
	stSecond
)

// Parse reads a duration such as "P3D", "PT48H", "P1Y2M10DT2H30M", "PT0.5S" or "-P2W".
func Parse(s string) (Duration, error) {
	var d Duration
	i := 0
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		d.Negative = s[i] == '-'
		i++
	}
	if i >= len(s) || s[i] != 'P' {
		return Duration{}, ErrSyntax
	}
	i++
	inTime, last, components, fractionSeen := false, 0, 0, false
	for i < len(s) {
		if s[i] == 'T' {
			if inTime || i+1 == len(s) {
				return Duration{}, ErrSyntax
			}
			inTime = true
			i++
			continue
		}
		// number: digits with an optional ',' or '.' fraction
		start := i
		var whole int64
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			whole = whole*10 + int64(s[i]-'0')
			if whole > 1<<31 {
				return Duration{}, ErrRange
			}
			i++
		}
		if i == start {
			return Duration{}, ErrSyntax
		}
		var frac int64 // nanoseconds-scaled fraction
		fracDigits := 0
		if i < len(s) && (s[i] == '.' || s[i] == ',') {
			i++
			scale := int64(100_000_000)
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				if fracDigits < 9 {
					frac += int64(s[i]-'0') * scale
					scale /= 10
				}
				fracDigits++
				i++
			}
			if fracDigits == 0 {
				return Duration{}, ErrSyntax
			}
		}
		if i >= len(s) || fractionSeen {
			if fractionSeen {
				return Duration{}, ErrFraction
			}
			return Duration{}, ErrSyntax
		}
		state, unit := designator(s[i], inTime)
		i++
		if state == 0 || state <= last {
			return Duration{}, ErrSyntax
		}
		if fracDigits > 0 {
			if !inTime {
				return Duration{}, ErrFraction
			}
			fractionSeen = true
		}
		last = state
		components++
		switch state {
		case stYear:
			d.Years = int32(whole)
		case stMonth:
			d.Months = int32(whole)
		case stWeek:
			d.Weeks = int32(whole)
		case stDay:
			d.Days = int32(whole)
		default:
			add := time.Duration(whole) * unit
			if whole != 0 && add/unit != time.Duration(whole) {
				return Duration{}, ErrRange
			}
			add += scaleFraction(frac, unit)
			if d.Clock > 0 && add > (1<<63-1)-d.Clock {
				return Duration{}, ErrRange
			}
			d.Clock += add
		}
	}
	if components == 0 || (d.Weeks != 0 && (d.Years|d.Months|d.Days != 0 || d.Clock != 0)) {
		return Duration{}, ErrSyntax
	}
	return d, nil
}

// scaleFraction computes frac/1e9 × unit exactly with a 128-bit product
// (0.5 hour in nanoseconds would overflow 64 bits as frac × unit).
func scaleFraction(frac int64, unit time.Duration) time.Duration {
	hi, lo := bits.Mul64(uint64(frac), uint64(unit))
	q, _ := bits.Div64(hi, lo, 1_000_000_000)
	return time.Duration(q)
}

func designator(c byte, inTime bool) (state int, unit time.Duration) {
	if !inTime {
		switch c {
		case 'Y':
			return stYear, 0
		case 'M':
			return stMonth, 0
		case 'W':
			return stWeek, 0
		case 'D':
			return stDay, 0
		}
		return 0, 0
	}
	switch c {
	case 'H':
		return stHour, time.Hour
	case 'M':
		return stMinute, time.Minute
	case 'S':
		return stSecond, time.Second
	}
	return 0, 0
}

// MustParse is Parse for constants known at compile time.
func MustParse(s string) Duration {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

// IsZero reports whether every component is zero.
func (d Duration) IsZero() bool {
	return d.Years == 0 && d.Months == 0 && d.Weeks == 0 && d.Days == 0 && d.Clock == 0
}

// Nominal reports whether the duration has calendar components.
func (d Duration) Nominal() bool { return d.Years != 0 || d.Months != 0 || d.Weeks != 0 || d.Days != 0 }

// Exact converts a duration without calendar components to time.Duration.
func (d Duration) Exact() (time.Duration, error) {
	if d.Nominal() {
		return 0, ErrNominal
	}
	if d.Negative {
		return -d.Clock, nil
	}
	return d.Clock, nil
}

// AddTo applies the duration to t: the calendar part through the calendar of
// t's location (so P1D across a DST change is still "same time tomorrow"),
// then the exact part.
func (d Duration) AddTo(t time.Time) time.Time {
	sign := 1
	if d.Negative {
		sign = -1
	}
	t = t.AddDate(sign*int(d.Years), sign*int(d.Months), sign*(7*int(d.Weeks)+int(d.Days)))
	return t.Add(time.Duration(sign) * d.Clock)
}

// String renders the canonical form, with seconds carrying any fraction.
func (d Duration) String() string {
	if d.IsZero() {
		return "PT0S"
	}
	b := make([]byte, 0, 24)
	if d.Negative {
		b = append(b, '-')
	}
	b = append(b, 'P')
	for _, c := range [...]struct {
		v int32
		u byte
	}{{d.Years, 'Y'}, {d.Months, 'M'}, {d.Weeks, 'W'}, {d.Days, 'D'}} {
		if c.v != 0 {
			b = strconv.AppendInt(b, int64(c.v), 10)
			b = append(b, c.u)
		}
	}
	if d.Clock != 0 {
		b = append(b, 'T')
		h := d.Clock / time.Hour
		m := (d.Clock % time.Hour) / time.Minute
		ns := d.Clock % time.Minute
		if h != 0 {
			b = strconv.AppendInt(b, int64(h), 10)
			b = append(b, 'H')
		}
		if m != 0 {
			b = strconv.AppendInt(b, int64(m), 10)
			b = append(b, 'M')
		}
		if ns != 0 {
			b = strconv.AppendInt(b, int64(ns/time.Second), 10)
			if f := ns % time.Second; f != 0 {
				frac := strconv.AppendInt(nil, int64(f)+1_000_000_000, 10)[1:]
				for frac[len(frac)-1] == '0' {
					frac = frac[:len(frac)-1]
				}
				b = append(append(b, '.'), frac...)
			}
			b = append(b, 'S')
		}
	}
	return string(b)
}

// MarshalText encodes the canonical form.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText decodes any accepted form.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := Parse(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "iso-8601-duration",
		Code:      "ISO 8601-1 (durações)",
		Title:     "Date and time — Representations for information interchange — durations",
		Authority: meta.ISO,
		Kind:      meta.Grammar,
		Edition:   "ISO 8601-1:2019, perfil da ABNF do RFC 3339 (apêndice A) com fração na parte de tempo",
		Package:   "iso/iso8601",
		Structure: "autômato determinístico escrito à mão, uma passada, sem regexp e sem alocação; separa duração nominal de exata",
		Verification: "tabela de formas válidas e inválidas da ABNF; fuzz de ida e volta (texto → valor → texto canônico → mesmo valor); " +
			"P1D atravessando horário de verão continua sendo o mesmo horário no dia seguinte",
		Sources: []meta.Source{
			{
				Name: "RFC 3339, Appendix A (ISO 8601 collected ABNF)", URL: "https://www.rfc-editor.org/rfc/rfc3339#appendix-A",
				Retrieved: "2026-10-14", License: "IETF Trust", Grounding: meta.Algorithmic,
			},
		},
	}
}
