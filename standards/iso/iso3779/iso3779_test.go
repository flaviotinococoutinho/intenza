package iso3779

import "testing"

func TestReferenceVIN(t *testing.T) {
	const vin = "1M8GDM9AXKP042788" // 49 CFR 565 / widely cited example, check digit X
	if !Valid(vin) || !CheckDigitValid(vin) {
		t.Fatal("reference VIN")
	}
	if WMI(vin) != "1M8" || VDS(vin) != "GDM9AX" || VIS(vin) != "KP042788" {
		t.Fatalf("sections: %s %s %s", WMI(vin), VDS(vin), VIS(vin))
	}
	if RegionOf(vin) != NorthAmerica {
		t.Fatal("1 is North America")
	}
	if a, b, ok := ModelYears(vin); !ok || a != 1989 || b != 2019 {
		t.Fatalf("model years %d %d", a, b)
	}
	if !CheckDigitValid("11111111111111111") {
		t.Fatal("all ones is valid")
	}
}

func TestSyntaxIsSeparateFromCheckDigit(t *testing.T) {
	brazil := "9BWZZZ377VT004251" // syntax only: Brazilian VINs need not carry the check digit
	if !Valid(brazil) || !Brazilian(brazil) || RegionOf(brazil) != SouthAmerica {
		t.Fatal("Brazilian WMI")
	}
	for _, bad := range []string{"1M8GDM9AXKP04278", "1M8GDM9AXKP0427880", "IM8GDM9AXKP042788", "1M8GDM9AXKP04278O", "1M8GDM9AXKP04278Q"} {
		if Valid(bad) {
			t.Errorf("%s accepted", bad)
		}
	}
	if Brazilian("8AWZZZ377VT004251") || Brazilian("9FWZZZ377VT004251") {
		t.Fatal("8A is Argentina, 9F Colombia")
	}
}

// Proof by exhaustion: every single substitution in a non-check position
// changes the computed check digit (weights are nonzero there and coprime to 11
// up to the transliteration collisions, which the test also enumerates).
func TestEverySubstitutionChangesTheCheckOrCollidesInTransliteration(t *testing.T) {
	base := []byte("1M8GDM9AXKP042788")
	want, _ := CheckDigit(string(base))
	alphabet := "0123456789ABCDEFGHJKLMNPRSTUVWXYZ"
	for pos := range 17 {
		if pos == 8 {
			continue
		}
		for i := range len(alphabet) {
			c := alphabet[i]
			if c == base[pos] {
				continue
			}
			m := append([]byte(nil), base...)
			m[pos] = c
			got, _ := CheckDigit(string(m))
			sameValue := translit[c] == translit[base[pos]]
			if (got == want) != sameValue {
				t.Fatalf("pos %d %c→%c: check %c vs %c, same transliteration %v", pos+1, base[pos], c, got, want, sameValue)
			}
		}
	}
}
