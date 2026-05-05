package primes

import (
	"math/rand/v2"
	"time"
)

// Seconds contains prime durations near 1 minute — moves background loops off the 60s cron boundary.
var Seconds = []time.Duration{
	59 * time.Second,
	61 * time.Second,
	67 * time.Second,
	71 * time.Second,
	73 * time.Second,
}

// Hours contains prime durations near 1 hour for coarser background loops.
var Hours = []time.Duration{
	3557 * time.Second,
	3571 * time.Second,
	3593 * time.Second,
	3607 * time.Second,
	3613 * time.Second,
}

// Pick returns a random element from pool.
func Pick(pool []time.Duration) time.Duration {
	// #nosec G404 -- Scheduling jitter, not security: math/rand/v2 is the right choice.
	return pool[rand.IntN(len(pool))]
}
