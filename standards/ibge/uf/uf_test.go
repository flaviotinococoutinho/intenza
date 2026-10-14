package uf

import "testing"

// Second witness: the official macro-region of each unit, written
// independently of the digit rule used by Region().
var officialRegion = map[string]string{
	"AC": "Norte", "AM": "Norte", "AP": "Norte", "PA": "Norte", "RO": "Norte", "RR": "Norte", "TO": "Norte",
	"AL": "Nordeste", "BA": "Nordeste", "CE": "Nordeste", "MA": "Nordeste", "PB": "Nordeste", "PE": "Nordeste",
	"PI": "Nordeste", "RN": "Nordeste", "SE": "Nordeste",
	"ES": "Sudeste", "MG": "Sudeste", "RJ": "Sudeste", "SP": "Sudeste",
	"PR": "Sul", "RS": "Sul", "SC": "Sul",
	"DF": "Centro-Oeste", "GO": "Centro-Oeste", "MS": "Centro-Oeste", "MT": "Centro-Oeste",
}

// Proof by exhaustion over the 27 units.
func TestEveryUnitRoundTripsAndAgreesWithTheOfficialRegion(t *testing.T) {
	n := 0
	for u := range All() {
		n++
		if got, ok := ByAbbrev(u.Abbrev()); !ok || got != u {
			t.Errorf("abbrev %s", u)
		}
		if got, ok := ByIBGE(u.IBGE()); !ok || got != u {
			t.Errorf("ibge %d", u.IBGE())
		}
		if u.Region().Name() != officialRegion[u.Abbrev()] {
			t.Errorf("%s: digit rule says %s, official list says %s", u, u.Region(), officialRegion[u.Abbrev()])
		}
		if u.ISO31662() != "BR-"+u.Abbrev() || u.Name() == "" {
			t.Errorf("%s: %s %q", u, u.ISO31662(), u.Name())
		}
	}
	if n != 27 || len(officialRegion) != 27 {
		t.Fatalf("27 units, got %d", n)
	}
}

func TestKnownUnits(t *testing.T) {
	if SP.Name() != "São Paulo" || SP.IBGE() != 35 || DF.Name() != "Distrito Federal" || TO.Name() != "Tocantins" {
		t.Fatalf("names: %q %d %q %q", SP.Name(), SP.IBGE(), DF.Name(), TO.Name())
	}
	if u, ok := ByAbbrev("rj"); !ok || u != RJ {
		t.Fatal("lowercase abbreviation")
	}
	for _, bad := range []string{"", "XX", "S", "SPX"} {
		if _, ok := ByAbbrev(bad); ok {
			t.Errorf("ByAbbrev(%q)", bad)
		}
	}
	if _, ok := ByIBGE(34); ok {
		t.Fatal("34 is not a unit")
	}
}
