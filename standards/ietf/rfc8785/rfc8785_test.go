package rfc8785

import (
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
)

// RFC 8785, Appendix B: every sample, as IEEE 754 bit patterns.
func TestNumberSamplesFromAppendixB(t *testing.T) {
	samples := map[string]string{
		"0000000000000000": "0",
		"8000000000000000": "0",
		"0000000000000001": "5e-324",
		"8000000000000001": "-5e-324",
		"7fefffffffffffff": "1.7976931348623157e+308",
		"ffefffffffffffff": "-1.7976931348623157e+308",
		"4340000000000000": "9007199254740992",
		"c340000000000000": "-9007199254740992",
		"4430000000000000": "295147905179352830000",
		"44b52d02c7e14af5": "9.999999999999997e+22",
		"44b52d02c7e14af6": "1e+23",
		"44b52d02c7e14af7": "1.0000000000000001e+23",
		"444b1ae4d6e2ef4e": "999999999999999700000",
		"444b1ae4d6e2ef4f": "999999999999999900000",
		"444b1ae4d6e2ef50": "1e+21",
		"3eb0c6f7a0b5ed8c": "9.999999999999997e-7",
		"3eb0c6f7a0b5ed8d": "0.000001",
		"41b3de4355555553": "333333333.3333332",
		"41b3de4355555554": "333333333.33333325",
		"41b3de4355555555": "333333333.3333333",
		"41b3de4355555556": "333333333.3333334",
		"41b3de4355555557": "333333333.33333343",
		"becbf647612f3696": "-0.0000033333333333333333",
		"43143ff3c1cb0959": "1424953923781206.2",
	}
	for bitsHex, want := range samples {
		u, _ := strconv.ParseUint(bitsHex, 16, 64)
		got, err := AppendNumber(nil, math.Float64frombits(u))
		if err != nil || string(got) != want {
			t.Errorf("%s: got %s (%v), want %s", bitsHex, got, err, want)
		}
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1)} {
		if _, err := AppendNumber(nil, bad); !errors.Is(err, ErrNumber) {
			t.Errorf("%v must be rejected", bad)
		}
	}
}

// RFC 8785, sections 3.2.2–3.2.4: the sample object and its exact bytes.
func TestSampleFromSection3(t *testing.T) {
	in := `{
	  "numbers": [333333333.33333329, 1E30, 4.50, 2e-3, 0.000000000000000000000000001],
	  "string": "\u20ac$\u000F\u000aA'\u0042\u0022\u005c\\\"\/",
	  "literals": [null, true, false]
	}`
	got, err := Transform([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"literals":[null,true,false],"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27],"string":"€$\u000f\nA'B\"\\\\\"/"}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	bytesHex := "7b 22 6c 69 74 65 72 61 6c 73 22 3a 5b 6e 75 6c 6c 2c 74 72 75 65 2c 66 61 6c 73 65 5d 2c 22 6e 75 6d 62 65 72 73 22 3a " +
		"5b 33 33 33 33 33 33 33 33 33 2e 33 33 33 33 33 33 33 2c 31 65 2b 33 30 2c 34 2e 35 2c 30 2e 30 30 32 2c 31 65 2d 32 37 " +
		"5d 2c 22 73 74 72 69 6e 67 22 3a 22 e2 82 ac 24 5c 75 30 30 30 66 5c 6e 41 27 42 5c 22 5c 5c 5c 5c 5c 22 2f 22 7d"
	if hex.EncodeToString(got) != strings.ReplaceAll(bytesHex, " ", "") {
		t.Fatalf("UTF-8 bytes differ from section 3.2.4")
	}
}

// RFC 8785, section 3.2.3: ordering by UTF-16 code units, not by bytes.
func TestPropertySortingByUTF16(t *testing.T) {
	in := `{"\u20ac":"Euro Sign","\r":"Carriage Return","\ufb33":"Hebrew Letter Dalet With Dagesh","1":"One",` +
		`"\ud83d\ude00":"Emoji: Grinning Face","\u0080":"Control","\u00f6":"Latin Small Letter O With Diaeresis"}`
	got, err := Transform([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	order := []string{
		"Carriage Return", "One", "Control", "Latin Small Letter O With Diaeresis", "Euro Sign",
		"Emoji: Grinning Face", "Hebrew Letter Dalet With Dagesh",
	}
	last := -1
	for _, v := range order {
		i := strings.Index(string(got), v)
		if i <= last {
			t.Fatalf("%q out of order in %s", v, got)
		}
		last = i
	}
}

func TestRejectsDuplicatesAndTrailingData(t *testing.T) {
	if _, err := Transform([]byte(`{"a":1,"a":2}`)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := Transform([]byte(`{"a":1} {}`)); err == nil {
		t.Fatal("trailing data accepted")
	}
	if _, err := Transform([]byte(`{"a":1e400}`)); !errors.Is(err, ErrNumber) {
		t.Fatal("out-of-range number accepted")
	}
}

func TestMarshalIsIndependentOfFieldOrder(t *testing.T) {
	type A struct {
		Z int    `json:"z"`
		A string `json:"a"`
	}
	type B struct {
		A string `json:"a"`
		Z int    `json:"z"`
	}
	x, _ := Marshal(A{Z: 1, A: "x"})
	y, _ := Marshal(B{A: "x", Z: 1})
	if string(x) != string(y) || string(x) != `{"a":"x","z":1}` {
		t.Fatalf("%s vs %s", x, y)
	}
}

func FuzzTransformIsIdempotent(f *testing.F) {
	for _, s := range []string{`{"b":[1,2.5,{"y":null,"x":true}],"a":"é\n"}`, `[1e21,1e-7,-0]`, `"\ud83d\ude00"`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		once, err := Transform([]byte(s))
		if err != nil {
			return
		}
		twice, err := Transform(once)
		if err != nil || string(twice) != string(once) {
			t.Fatalf("not idempotent: %s → %s (%v)", once, twice, err)
		}
	})
}
