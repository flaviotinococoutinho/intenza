//intenza:paradigm data-oriented

// Package fold normalizes text for search: lowercase ASCII, Latin diacritics
// stripped by table lookup ("São Tomé" → "sao tome"), anything else that is
// not a letter or digit becomes a single space. Generators and lookups share it,
// so an index built offline always agrees with a query folded at run time.
package fold

import (
	"unicode"
	"unicode/utf8"
)

// latin maps U+00C0..U+017F to an ASCII base letter. '_' separates, '*' marks
// a digraph handled by digraph().
const latin = "" +
	"aaaaaa*ceeeeiiii" + // U+00C0
	"dnooooo_ouuuuy**" + // U+00D0
	"aaaaaa*ceeeeiiii" + // U+00E0
	"dnooooo_ouuuuy*y" + // U+00F0
	"aaaaaaccccccccdd" + // U+0100
	"ddeeeeeeeeeegggg" + // U+0110
	"gggghhhhiiiiiiii" + // U+0120
	"ii**jjkkklllllll" + // U+0130
	"lllnnnnnnnnnoooo" + // U+0140
	"oo**rrrrrrssssss" + // U+0150
	"ssttttttuuuuuuuu" + // U+0160
	"uuuuwwyyyzzzzzzs" //   U+0170

func digraph(r rune) string {
	switch r {
	case 'Æ', 'æ':
		return "ae"
	case 'Þ', 'þ':
		return "th"
	case 'ß':
		return "ss"
	case 'Ĳ', 'ĳ':
		return "ij"
	case 'Œ', 'œ':
		return "oe"
	}
	return ""
}

// Append folds s and appends it to dst. With a stack buffer as dst it does not
// allocate for short inputs.
func Append(dst []byte, s string) []byte {
	space := true // suppress leading and repeated separators
	sep := func() {
		if !space {
			dst = append(dst, ' ')
			space = true
		}
	}
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			i++
			switch {
			case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
				dst, space = append(dst, c), false
			case c >= 'A' && c <= 'Z':
				dst, space = append(dst, c|0x20), false
			default:
				sep()
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r >= 0xC0 && r <= 0x17F {
			switch m := latin[r-0xC0]; m {
			case '_':
				sep()
			case '*':
				dst, space = append(dst, digraph(r)...), false
			default:
				dst, space = append(dst, m), false
			}
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			dst, space = utf8.AppendRune(dst, unicode.ToLower(r)), false
			continue
		}
		sep()
	}
	if n := len(dst); n > 0 && dst[n-1] == ' ' {
		dst = dst[:n-1]
	}
	return dst
}

// String folds s into a new string.
func String(s string) string { return string(Append(make([]byte, 0, len(s)), s)) }
