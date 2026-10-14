//intenza:paradigm functional-core

// Package rfc9562 generates and parses UUID version 7: 48 bits of Unix
// milliseconds, a 12-bit counter and 62 random bits. Time-ordered IDs keep
// B-tree inserts near the right edge of the index instead of scattering them.
//
// The generator has no hidden inputs: the clock and the entropy source are
// injected, so the package stays pure and tests are deterministic. Monotonicity
// follows RFC 9562 section 6.2, method 1 (fixed-length dedicated counter):
// within one millisecond, or if the clock steps back, the counter increments;
// when it overflows, the timestamp is advanced by one millisecond.
package rfc9562

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/flaviotinococoutinho/intenza/standards/meta"
)

// UUID is a 128-bit identifier.
type UUID [16]byte

var ErrSyntax = errors.New("rfc9562: UUID must be 8-4-4-4-12 hex digits")

// Generator produces monotonic version 7 UUIDs.
type Generator struct {
	mu      sync.Mutex
	now     func() time.Time
	entropy io.Reader
	lastMS  int64
	counter uint16
}

// NewGenerator wires a clock and an entropy source (time.Now and crypto/rand
// in production, fixed ones in tests).
func NewGenerator(now func() time.Time, entropy io.Reader) *Generator {
	return &Generator{now: now, entropy: entropy}
}

// New returns the next UUIDv7.
func (g *Generator) New() (UUID, error) {
	var u UUID
	var rnd [10]byte
	if _, err := io.ReadFull(g.entropy, rnd[:]); err != nil {
		return u, err
	}
	g.mu.Lock()
	ms := g.now().UnixMilli()
	if ms > g.lastMS {
		g.lastMS = ms
		g.counter = uint16(rnd[0]&0x07)<<8 | uint16(rnd[1]) // random start, top bit clear: room to grow
	} else {
		g.counter++
		if g.counter > 0x0FFF {
			g.lastMS++ // borrow the next millisecond rather than repeat or go back
			g.counter = 0
		}
	}
	ms, counter := g.lastMS, g.counter
	g.mu.Unlock()

	u[0], u[1], u[2], u[3], u[4], u[5] = byte(ms>>40), byte(ms>>32), byte(ms>>24), byte(ms>>16), byte(ms>>8), byte(ms)
	u[6] = 0x70 | byte(counter>>8) // version 7
	u[7] = byte(counter)
	u[8] = 0x80 | rnd[2]&0x3F // variant 10
	copy(u[9:], rnd[3:])
	return u, nil
}

// Version is the version nibble.
func (u UUID) Version() int { return int(u[6] >> 4) }

// Time is the embedded Unix millisecond timestamp.
func (u UUID) Time() time.Time {
	ms := int64(u[0])<<40 | int64(u[1])<<32 | int64(u[2])<<24 | int64(u[3])<<16 | int64(u[4])<<8 | int64(u[5])
	return time.UnixMilli(ms)
}

const hexDigits = "0123456789abcdef"

// AppendText appends the canonical lowercase form without allocating.
func (u UUID) AppendText(b []byte) ([]byte, error) {
	for i, x := range u {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			b = append(b, '-')
		}
		b = append(b, hexDigits[x>>4], hexDigits[x&0xF])
	}
	return b, nil
}

// String is the canonical 8-4-4-4-12 form.
func (u UUID) String() string {
	var b [36]byte
	out, _ := u.AppendText(b[:0])
	return string(out)
}

// MarshalText encodes the canonical form.
func (u UUID) MarshalText() ([]byte, error) { return u.AppendText(make([]byte, 0, 36)) }

// UnmarshalText decodes the canonical form, in either case.
func (u *UUID) UnmarshalText(b []byte) error {
	v, err := Parse(string(b))
	if err != nil {
		return err
	}
	*u = v
	return nil
}

func nibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// Parse reads the canonical 36-character form of any UUID version.
func Parse(s string) (UUID, error) {
	var u UUID
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return u, ErrSyntax
	}
	j := 0
	for i := 0; i < 36; {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			i++
			continue
		}
		hi, ok1 := nibble(s[i])
		lo, ok2 := nibble(s[i+1])
		if !ok1 || !ok2 {
			return UUID{}, ErrSyntax
		}
		u[j] = hi<<4 | lo
		j++
		i += 2
	}
	return u, nil
}

// Spec is the epistemic record of this SDK.
func Spec() meta.Standard {
	return meta.Standard{
		ID:        "ietf-rfc-9562",
		Code:      "RFC 9562",
		Title:     "Universally Unique IDentifiers (UUIDs) — versão 7",
		Authority: meta.IETF,
		Kind:      meta.Grammar,
		Edition:   "RFC 9562 (maio de 2024), seção 5.7 e método 1 da seção 6.2",
		Package:   "ietf/rfc9562",
		Structure: "relógio e entropia injetados; contador de 12 bits com início aleatório; formatação sem alocação",
		Verification: "versão e variante nos bits certos; ordem estrita com relógio parado, voltando e estourando o contador; " +
			"exemplo do apêndice A.6 interpretado",
		Sources: []meta.Source{
			{
				Name: "RFC 9562", URL: "https://www.rfc-editor.org/rfc/rfc9562", Retrieved: "2026-10-14",
				License: "IETF Trust", Grounding: meta.Algorithmic,
			},
		},
	}
}
