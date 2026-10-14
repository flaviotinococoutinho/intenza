package brcode

import (
	"errors"
	"testing"

	"github.com/flaviotinococoutinho/intenza/standards/iso/iso18245"
)

// The example payload of the BCB manual.
const manual = "00020126580014br.gov.bcb.pix0136123e4567-e12b-12d1-a456-4266554400005204000053039865802BR5913Fulano de Tal6008BRASILIA62070503***63041D3D"

func TestManualExample(t *testing.T) {
	p, err := Decode(manual)
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "123e4567-e12b-12d1-a456-426655440000" || p.MerchantName != "Fulano de Tal" || p.MerchantCity != "BRASILIA" || p.TxID != "***" {
		t.Fatalf("%+v", p)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	mcc, _ := iso18245.Lookup(5945)
	in := Pix{
		Location: "psp.example/qr/v2/9d36b84f", MCC: mcc, AmountCents: 123456, MerchantName: "INTENZA ESCROW",
		MerchantCity: "SAO PAULO", TxID: "AGR0001", Dynamic: true,
	}
	s, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(s)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round trip:\n%+v\n%+v", out, in)
	}
}

func TestRejects(t *testing.T) {
	if _, err := Encode(Pix{Key: "k", Location: "l", MerchantName: "x", MerchantCity: "y"}); !errors.Is(err, ErrField) {
		t.Fatal("key and location are exclusive")
	}
	if _, err := Encode(Pix{Key: "k", MerchantName: "a name longer than twenty-five chars", MerchantCity: "y"}); !errors.Is(err, ErrField) {
		t.Fatal("merchant name limit")
	}
}
