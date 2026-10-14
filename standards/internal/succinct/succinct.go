//intenza:paradigm data-oriented

// Package succinct holds the static structures behind every code list in this
// module. They are built once by the generators and compiled as pointer-free
// arrays, so lookups touch a handful of cache lines, allocate nothing and give
// the garbage collector nothing to scan.
package succinct

import (
	"bytes"
	"iter"
	"math/bits"
)

// Rank is a static rank dictionary over a dense integer universe: one bit per
// possible key and the cumulative popcount before each 64-bit word.
//
// Index(k) costs one load, one mask and one popcount, whatever the size of the
// set. A three-letter code (26³ = 17 576 keys) fits in 2.2 KB of bits plus
// 550 bytes of ranks: smaller than a hash map and without hashing.
type Rank struct {
	Words []uint64
	Ranks []uint16
}

// Index returns the position of k among the present keys, in key order.
func (r Rank) Index(k uint32) (int, bool) {
	w := k >> 6
	if int(w) >= len(r.Words) {
		return 0, false
	}
	word := r.Words[w]
	bit := uint64(1) << (k & 63)
	if word&bit == 0 {
		return 0, false
	}
	return int(r.Ranks[w]) + bits.OnesCount64(word&(bit-1)), true
}

// Len is the number of present keys.
func (r Rank) Len() int {
	if len(r.Words) == 0 {
		return 0
	}
	last := len(r.Words) - 1
	return int(r.Ranks[last]) + bits.OnesCount64(r.Words[last])
}

// Build creates a Rank for the given keys over [0, universe). Generators use it;
// at run time only Index is called.
func Build(universe uint32, keys []uint32) Rank {
	n := (universe + 63) / 64
	r := Rank{Words: make([]uint64, n), Ranks: make([]uint16, n)}
	for _, k := range keys {
		r.Words[k>>6] |= 1 << (k & 63)
	}
	var acc uint16
	for i, w := range r.Words {
		r.Ranks[i] = acc
		acc += uint16(bits.OnesCount64(w))
	}
	return r
}

// Alpha2 packs two ASCII letters (either case) into [0, 676).
func Alpha2(s string) (uint32, bool) {
	if len(s) != 2 {
		return 0, false
	}
	a, ok1 := letter(s[0])
	b, ok2 := letter(s[1])
	return a*26 + b, ok1 && ok2
}

// Alpha3 packs three ASCII letters (either case) into [0, 17 576).
func Alpha3(s string) (uint32, bool) {
	if len(s) != 3 {
		return 0, false
	}
	a, ok1 := letter(s[0])
	b, ok2 := letter(s[1])
	c, ok3 := letter(s[2])
	return (a*26+b)*26 + c, ok1 && ok2 && ok3
}

func letter(c byte) (uint32, bool) {
	c |= 0x20 // ASCII lowercase; non-letters fail the range check below
	if c < 'a' || c > 'z' {
		return 0, false
	}
	return uint32(c - 'a'), true
}

// Digits parses 1 to 4 ASCII digits without allocating.
func Digits(s string) (uint32, bool) {
	if len(s) == 0 || len(s) > 4 {
		return 0, false
	}
	var n uint32
	for i := range len(s) {
		d := s[i] - '0'
		if d > 9 {
			return 0, false
		}
		n = n*10 + uint32(d)
	}
	return n, true
}

// Tokens is a static inverted index for prefix search: folded tokens sorted
// in one string blob, their offsets and the record each token points to.
type Tokens struct {
	Blob string
	Off  []uint32 // len(Post)+1 offsets into Blob
	Post []uint16 // record index of each token
}

func (t Tokens) token(i int) string { return t.Blob[t.Off[i]:t.Off[i+1]] }

// lowerBound finds the first token >= q by binary search over the offsets.
// string(q) inside a comparison does not allocate.
func (t Tokens) lowerBound(q []byte) int {
	lo, hi := 0, len(t.Post)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if t.token(mid) < string(q) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func hasPrefix(tok string, q []byte) bool { return len(tok) >= len(q) && tok[:len(q)] == string(q) }

// Set is a fixed bitset over record indexes, small enough to live on the stack.
type Set [16]uint64 // up to 1 024 records

// Has reports whether i is in the set.
func (s *Set) Has(i int) bool { return s[i>>6]&(1<<(i&63)) != 0 }

// All yields the members in increasing order, skipping empty words.
func (s *Set) All() iter.Seq[int] {
	return func(yield func(int) bool) {
		for w, word := range s {
			for word != 0 {
				if !yield(w<<6 + bits.TrailingZeros64(word)) {
					return
				}
				word &= word - 1
			}
		}
	}
}

// Match returns the records having, for every query token, a token with that
// prefix (AND of prefixes). The query must already be folded.
func (t Tokens) Match(folded []byte) (Set, bool) {
	var acc Set
	first := true
	for len(folded) > 0 {
		// folded text has single spaces and no edges: split by hand to keep
		// the query on the caller's stack (an iterator closure would escape it)
		end := bytes.IndexByte(folded, ' ')
		if end < 0 {
			end = len(folded)
		}
		q := folded[:end]
		folded = folded[min(end+1, len(folded)):]
		if len(q) == 0 {
			continue
		}
		var cur Set
		for i := t.lowerBound(q); i < len(t.Post) && hasPrefix(t.token(i), q); i++ {
			r := int(t.Post[i])
			cur[r>>6] |= 1 << (r & 63)
		}
		if first {
			acc, first = cur, false
			continue
		}
		for w := range acc {
			acc[w] &= cur[w]
		}
	}
	return acc, !first
}
