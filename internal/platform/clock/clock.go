//intenza:paradigm imperative-shell

// Package clock is the node's local clock, with an adjustable skew so chaos
// experiments can push one node into the future and check that nothing that
// matters trusts it.
package clock

import (
	"sync/atomic"
	"time"
)

// System is the wall clock plus a skew.
type System struct{ skew atomic.Int64 }

// Now returns the skewed wall clock.
func (c *System) Now() time.Time { return time.Now().Add(time.Duration(c.skew.Load())) }

// SetSkew moves the clock (0 restores it).
func (c *System) SetSkew(d time.Duration) { c.skew.Store(int64(d)) }

// Skew reports the current skew.
func (c *System) Skew() time.Duration { return time.Duration(c.skew.Load()) }
