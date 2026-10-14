package cep

import (
	"testing"

	"github.com/flaviotinococoutinho/intenza/standards/ibge/uf"
)

func TestCapitalsFallInTheirUnit(t *testing.T) {
	cases := map[string]uf.UF{
		"01310-100": uf.SP, "20040-020": uf.RJ, "29010-120": uf.ES, "30130-010": uf.MG,
		"40020-000": uf.BA, "49010-020": uf.SE, "50030-230": uf.PE, "57020-050": uf.AL,
		"58010-000": uf.PB, "59012-300": uf.RN, "60060-000": uf.CE, "64000-020": uf.PI,
		"65010-000": uf.MA, "66010-000": uf.PA, "68900-073": uf.AP, "69005-070": uf.AM,
		"69301-000": uf.RR, "69900-062": uf.AC, "70040-010": uf.DF, "74003-010": uf.GO,
		"76801-000": uf.RO, "77001-002": uf.TO, "78005-000": uf.MT, "79002-000": uf.MS,
		"80010-000": uf.PR, "88010-000": uf.SC, "90010-000": uf.RS,
	}
	seen := map[uf.UF]bool{}
	for s, want := range cases {
		c, err := Parse(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		got, ok := c.UF()
		if !ok || got != want {
			t.Errorf("%s: %v, want %v", s, got, want)
		}
		seen[want] = true
		if c.String() != s {
			t.Errorf("round trip %s → %s", s, c)
		}
	}
	if len(seen) != 27 {
		t.Fatalf("every unit has a capital here, got %d", len(seen))
	}
}

func TestRangesDoNotOverlapAndGapsStayUnassigned(t *testing.T) {
	var owner [1000]uf.UF
	for _, r := range ranges {
		for p := r.lo; p <= r.hi; p++ {
			if owner[p] != 0 {
				t.Fatalf("prefix %03d claimed by %v and %v", p, owner[p], r.u)
			}
			owner[p] = r.u
		}
	}
	for _, s := range []string{"00999-999", "78900-000"} {
		c, _ := Parse(s)
		if _, ok := c.UF(); ok {
			t.Errorf("%s should be unassigned", s)
		}
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	for _, s := range []string{"", "0131010", "013101000", "01310_100", "0131-0100", "01310-10a"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}
