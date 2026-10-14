package mpm

import (
	"errors"
	"testing"
)

func TestCRCCheckValue(t *testing.T) {
	if got := CRC16("123456789"); got != 0x29B1 {
		t.Fatalf("CRC-16/CCITT-FALSE check = %04X", got)
	}
}

func TestParseSplitAndSeal(t *testing.T) {
	const bcb = "00020126580014br.gov.bcb.pix0136123e4567-e12b-12d1-a456-4266554400005204000053039865802BR5913Fulano de Tal6008BRASILIA62070503***63041D3D"
	p, err := Parse(bcb)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := p.Get(59); v != "Fulano de Tal" {
		t.Fatalf("merchant name %q", v)
	}
	acct, ok := p.Sub(26)
	if !ok {
		t.Fatal("template 26")
	}
	if gui, _ := acct.Get(0); gui != "br.gov.bcb.pix" {
		t.Fatalf("gui %q", gui)
	}
	again, err := p.Seal()
	if err != nil || again != bcb {
		t.Fatalf("seal:\n%s\n%s", again, bcb)
	}
	if _, err := Parse(bcb[:len(bcb)-1] + "E"); !errors.Is(err, ErrCRC) {
		t.Fatalf("tampered CRC: %v", err)
	}
	if _, err := Parse("0002010"); !errors.Is(err, ErrSyntax) {
		t.Fatalf("truncated: %v", err)
	}
}

func FuzzSealParse(f *testing.F) {
	f.Add("01", "x")
	f.Fuzz(func(t *testing.T, a, b string) {
		if len(a) > 99 || len(b) > 99 {
			return
		}
		s, err := Payload{{0, "01"}, {26, a}, {59, b}}.Seal()
		if err != nil {
			t.Fatal(err)
		}
		p, err := Parse(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		if v, _ := p.Get(59); v != b {
			t.Fatalf("value changed: %q vs %q", v, b)
		}
	})
}
