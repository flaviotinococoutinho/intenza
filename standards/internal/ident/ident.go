//intenza:paradigm functional-core

// Package ident holds the normalization shared by identifier SDKs: copy the
// significant characters into a caller-owned buffer, dropping the separators
// people type (dots, hyphens, slashes, spaces).
package ident

// Compact appends the significant bytes of s to dst, uppercasing ASCII letters
// when letters is true. It fails on any other byte or when dst would grow past
// its capacity, so a stack array as dst never escapes.
func Compact(dst []byte, s string, letters bool) ([]byte, bool) {
	for i := range len(s) {
		c := s[i]
		switch {
		case c == '.' || c == '-' || c == '/' || c == ' ':
			continue
		case c >= '0' && c <= '9':
		case letters && c >= 'a' && c <= 'z':
			c -= 'a' - 'A'
		case letters && c >= 'A' && c <= 'Z':
		default:
			return dst, false
		}
		if len(dst) == cap(dst) {
			return dst, false
		}
		dst = append(dst, c)
	}
	return dst, true
}

// AllSame reports whether every byte of b is the same (000.000.000-00 and
// friends pass the arithmetic but are not issued).
func AllSame(b []byte) bool {
	for i := 1; i < len(b); i++ {
		if b[i] != b[0] {
			return false
		}
	}
	return len(b) > 0
}

// Mod11 is the weighted remainder used by Brazilian registries: the digit is
// 11 minus the remainder, and remainders 0 and 1 both collapse to 0.
func Mod11(sum int) byte {
	r := sum % 11
	if r < 2 {
		return 0
	}
	return byte(11 - r)
}
