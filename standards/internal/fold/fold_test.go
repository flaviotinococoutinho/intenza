package fold

import "testing"

func TestFold(t *testing.T) {
	cases := map[string]string{
		"São Tomé e Príncipe":   "sao tome e principe",
		"Côte d’Ivoire":         "cote d ivoire",
		"  Hobby, Toy, & Game ": "hobby toy game",
		"ŒUVRE ß Æsir Þor":      "oeuvre ss aesir thor",
		"Ünited  ×  Ŝtates":     "united states",
		"日本":                    "日本",
		"already folded":        "already folded",
		"":                      "",
	}
	for in, want := range cases {
		if got := String(in); got != want {
			t.Errorf("fold(%q) = %q, want %q", in, got, want)
		}
	}
}

// Folding is idempotent: folding a folded string changes nothing.
func TestFoldIsIdempotent(t *testing.T) {
	for _, s := range []string{"Ação", "Reino Unido da Grã-Bretanha", "Ærø", "ŉ"} {
		once := String(s)
		if twice := String(once); twice != once {
			t.Errorf("fold not idempotent on %q: %q then %q", s, once, twice)
		}
	}
}

func TestAppendDoesNotAllocateWithStackBuffer(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		var buf [64]byte
		_ = Append(buf[:0], "Real Brasileiro")
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v", allocs)
	}
}

func FuzzFoldIsIdempotent(f *testing.F) {
	for _, s := range []string{"São Paulo", "Œ", "a b", "ǅ", "\xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		once := String(s)
		if String(once) != once {
			t.Fatalf("not idempotent: %q", s)
		}
	})
}
