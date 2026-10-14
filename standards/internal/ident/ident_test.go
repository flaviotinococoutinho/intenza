package ident

import "testing"

func TestCompact(t *testing.T) {
	var buf [14]byte
	got, ok := Compact(buf[:0], "12.abc.345/01de-35", true)
	if !ok || string(got) != "12ABC34501DE35" {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := Compact(buf[:0], "123.456.789-09", false); !ok {
		t.Fatal("digits with separators")
	}
	if _, ok := Compact(buf[:0], "12A", false); ok {
		t.Fatal("letters rejected when not allowed")
	}
	if _, ok := Compact(buf[:0], "123456789012345", false); ok {
		t.Fatal("overflow rejected")
	}
	if _, ok := Compact(buf[:0], "12_3", false); ok {
		t.Fatal("unknown separator rejected")
	}
	allocs := testing.AllocsPerRun(100, func() {
		var b [11]byte
		_, _ = Compact(b[:0], "529.982.247-25", false)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v", allocs)
	}
}

func TestMod11CollapsesZeroAndOne(t *testing.T) {
	for sum, want := range map[int]byte{0: 0, 1: 0, 11: 0, 12: 0, 2: 9, 13: 9, 10: 1} {
		if got := Mod11(sum); got != want {
			t.Errorf("Mod11(%d) = %d, want %d", sum, got, want)
		}
	}
}
