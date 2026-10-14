package relay

import "testing"

func TestCrashKnobWaitsForTheChosenType(t *testing.T) {
	batch := []doc{{Type: "intenza.proposal.submitted.v1"}, {Type: "intenza.intent.published.v1"}}
	set := func(r *Relay, v string) { r.CrashAfter.Store(&v) }
	r := &Relay{}
	if r.crashDue(batch) {
		t.Fatal("off by default")
	}
	for v, want := range map[string]bool{
		"":                              false,
		"*":                             true,
		"intenza.intent.published.v1":   true,
		"intenza.agreement.formed.v1":   false,
		"intenza.proposal.submitted.v1": true,
	} {
		set(r, v)
		if got := r.crashDue(batch); got != want {
			t.Errorf("knob %q: crash=%v, want %v", v, got, want)
		}
	}
}
