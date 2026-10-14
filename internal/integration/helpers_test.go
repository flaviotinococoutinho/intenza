//go:build integration

package integration

import "github.com/flaviotinococoutinho/intenza/standards/iso/iso8601"

func iso8601Must(s string) iso8601.Duration { return iso8601.MustParse(s) }
