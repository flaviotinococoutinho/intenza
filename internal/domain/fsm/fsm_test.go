package fsm

import (
	"errors"
	"strings"
	"testing"
)

type (
	st string
	ev string
)

func TestMachine(t *testing.T) {
	m := New("door", st("closed"),
		Transition[st, ev]{From: "closed", On: "open", To: "opened"},
		Transition[st, ev]{From: "opened", On: "close", To: "closed"},
		Transition[st, ev]{From: "closed", On: "lock", To: "locked"},
	)
	if to, err := m.Next("closed", "open"); err != nil || to != "opened" {
		t.Fatal(to, err)
	}
	if _, err := m.Next("locked", "open"); !errors.Is(err, ErrTransition) {
		t.Fatal("locked door does not open")
	}
	if !m.Terminal("locked") || m.Terminal("closed") {
		t.Fatal("terminal states")
	}
	if got := m.Reachable(); len(got) != 3 {
		t.Fatalf("reachable %v", got)
	}
	if d := m.Mermaid(); !strings.Contains(d, "closed --> locked: lock") || !strings.Contains(d, "locked --> [*]") {
		t.Fatalf("mermaid:\n%s", d)
	}
}

func TestNondeterministicTablePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("two edges with the same event from the same state must not build")
		}
	}()
	New("bad", st("a"), Transition[st, ev]{"a", "x", "b"}, Transition[st, ev]{"a", "x", "c"})
}
