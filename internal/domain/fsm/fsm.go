//intenza:paradigm automata

// Package fsm is a table-driven finite state machine. Lifecycles are data:
// the table answers "may this happen now", lists every state for the
// ontology's phase partition and draws itself for the documentation.
package fsm

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrTransition is wrapped by every refused transition.
var ErrTransition = errors.New("fsm: transition not allowed")

// Transition is one edge of the machine.
type Transition[S, E ~string] struct {
	From S
	On   E
	To   S
}

// Machine is an immutable transition table.
type Machine[S, E ~string] struct {
	name    string
	initial S
	edges   []Transition[S, E]
	next    map[S]map[E]S
}

// New builds a machine from its initial state and edges.
func New[S, E ~string](name string, initial S, edges ...Transition[S, E]) *Machine[S, E] {
	m := &Machine[S, E]{name: name, initial: initial, edges: edges, next: map[S]map[E]S{}}
	for _, t := range edges {
		if m.next[t.From] == nil {
			m.next[t.From] = map[E]S{}
		}
		if _, dup := m.next[t.From][t.On]; dup {
			panic(fmt.Sprintf("fsm %s: nondeterministic edge %s --%s-->", name, t.From, t.On))
		}
		m.next[t.From][t.On] = t.To
	}
	return m
}

// Next returns the state after event on in state from.
func (m *Machine[S, E]) Next(from S, on E) (S, error) {
	if to, ok := m.next[from][on]; ok {
		return to, nil
	}
	return from, fmt.Errorf("%w: %s não aceita %q no estado %q", ErrTransition, m.name, on, from)
}

// Can reports whether the event is accepted in the state.
func (m *Machine[S, E]) Can(from S, on E) bool {
	_, ok := m.next[from][on]
	return ok
}

// Terminal reports whether no event leaves the state.
func (m *Machine[S, E]) Terminal(s S) bool { return len(m.next[s]) == 0 }

// States lists every state, sorted.
func (m *Machine[S, E]) States() []S {
	set := map[S]bool{m.initial: true}
	for _, t := range m.edges {
		set[t.From], set[t.To] = true, true
	}
	out := make([]S, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// Reachable lists the states reachable from the initial one.
func (m *Machine[S, E]) Reachable() []S {
	seen := map[S]bool{m.initial: true}
	queue := []S{m.initial}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		for _, to := range m.next[s] {
			if !seen[to] {
				seen[to] = true
				queue = append(queue, to)
			}
		}
	}
	out := make([]S, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// Name is the machine's name.
func (m *Machine[S, E]) Name() string { return m.name }

// Mermaid draws the machine as a Mermaid state diagram.
func (m *Machine[S, E]) Mermaid() string {
	var b strings.Builder
	b.WriteString("stateDiagram-v2\n")
	fmt.Fprintf(&b, "  [*] --> %s\n", m.initial)
	for _, t := range m.edges {
		fmt.Fprintf(&b, "  %s --> %s: %s\n", t.From, t.To, t.On)
	}
	for _, s := range m.States() {
		if m.Terminal(s) {
			fmt.Fprintf(&b, "  %s --> [*]\n", s)
		}
	}
	return b.String()
}
