//intenza:paradigm imperative-shell

// Package ids mints UUIDv7 with the real clock and crypto entropy, and derives
// deterministic UUIDv8 from idempotency keys.
package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"time"

	"github.com/flaviotinococoutinho/intenza/standards/ietf/rfc9562"
)

// Generator implements app.IDs.
type Generator struct{ g *rfc9562.Generator }

// New wires the RFC 9562 generator to the system clock and crypto/rand.
func New() *Generator { return &Generator{g: rfc9562.NewGenerator(time.Now, rand.Reader)} }

// New returns a time-ordered UUIDv7.
func (g *Generator) New() string {
	u, err := g.g.New()
	if err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return u.String()
}

// Derived returns the same UUIDv8 for the same namespace and key.
func (g *Generator) Derived(namespace, key string) string {
	sum := sha256.Sum256([]byte("intenza/" + namespace + "/" + key))
	u, _ := rfc9562.FromHash(sum[:])
	return u.String()
}
