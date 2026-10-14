package iso8601

import (
	"errors"
	"testing"
	"time"
)

func TestParseValidForms(t *testing.T) {
	cases := map[string]Duration{
		"P3D":              {Days: 3},
		"PT48H":            {Clock: 48 * time.Hour},
		"P1Y2M10DT2H30M":   {Years: 1, Months: 2, Days: 10, Clock: 2*time.Hour + 30*time.Minute},
		"P2W":              {Weeks: 2},
		"-P1D":             {Days: 1, Negative: true},
		"PT0.5S":           {Clock: 500 * time.Millisecond},
		"PT1,5H":           {Clock: 90 * time.Minute},
		"PT1M0.000000001S": {Clock: time.Minute + 1},
		"P0D":              {},
		"PT20S":            {Clock: 20 * time.Second},
	}
	for in, want := range cases {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
}

func TestParseRejectsInvalidForms(t *testing.T) {
	cases := map[string]error{
		"":              ErrSyntax,
		"P":             ErrSyntax,
		"PT":            ErrSyntax,
		"P1DT":          ErrSyntax,
		"3D":            ErrSyntax,
		"P1D2Y":         ErrSyntax, // out of order
		"P1Y1Y":         ErrSyntax, // repeated
		"P1W2D":         ErrSyntax, // weeks only alone
		"PT1H2D":        ErrSyntax,
		"P1.5D":         ErrFraction,
		"PT1.5H2M":      ErrFraction,
		"PT.5S":         ErrSyntax,
		"PT1.S":         ErrSyntax,
		"P1X":           ErrSyntax,
		"P99999999999D": ErrRange,
	}
	for in, want := range cases {
		if _, err := Parse(in); !errors.Is(err, want) {
			t.Errorf("Parse(%q) error = %v, want %v", in, err, want)
		}
	}
}

func TestNominalVersusExact(t *testing.T) {
	if _, err := MustParse("P1M").Exact(); !errors.Is(err, ErrNominal) {
		t.Fatal("a month has no fixed length")
	}
	if d, _ := MustParse("-PT90M").Exact(); d != -90*time.Minute {
		t.Fatalf("exact = %v", d)
	}
	sp, _ := time.LoadLocation("America/Sao_Paulo")
	if sp == nil {
		t.Skip("no tzdata")
	}
	// 2018-11-04 had a DST transition in São Paulo: P1D keeps the wall clock, PT24H does not.
	start := time.Date(2018, 11, 3, 12, 0, 0, 0, sp)
	if got := MustParse("P1D").AddTo(start); got.Hour() != 12 || got.Day() != 4 {
		t.Fatalf("P1D across DST = %v", got)
	}
	if got := MustParse("PT24H").AddTo(start); got.Hour() == 12 {
		t.Fatalf("PT24H across DST should shift the wall clock, got %v", got)
	}
	if got := MustParse("P1M").AddTo(time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)); got.Month() != time.March {
		t.Fatalf("Go normalizes Jan 31 + 1 month to March: %v", got)
	}
}

func TestStringIsCanonical(t *testing.T) {
	cases := map[string]string{"PT90M": "PT1H30M", "P0D": "PT0S", "PT1,500S": "PT1.5S", "-P2W": "-P2W", "P1Y2M10DT2H30M": "P1Y2M10DT2H30M"}
	for in, want := range cases {
		if got := MustParse(in).String(); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
}

func TestParseDoesNotAllocate(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() { _, _ = Parse("P1Y2M10DT2H30M0.25S") })
	if allocs != 0 {
		t.Fatalf("allocs = %v", allocs)
	}
}

func FuzzRoundTrip(f *testing.F) {
	for _, s := range []string{"P3D", "PT48H", "P1Y2M10DT2H30M", "PT0.5S", "-P2W", "P0D"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := Parse(s)
		if err != nil {
			return
		}
		back, err := Parse(d.String())
		if err != nil {
			t.Fatalf("%q → %q does not parse: %v", s, d.String(), err)
		}
		if back.Years != d.Years || back.Months != d.Months || back.Weeks != d.Weeks || back.Days != d.Days || back.Clock != d.Clock {
			t.Fatalf("%q → %q changed the value", s, d.String())
		}
	})
}

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = Parse("P1Y2M10DT2H30M0.25S")
	}
}
