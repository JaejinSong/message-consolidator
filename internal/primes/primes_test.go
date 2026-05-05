package primes_test

import (
	"message-consolidator/internal/primes"
	"testing"
	"time"
)

func TestSeconds_OnlyContainsExpectedPrimes(t *testing.T) {
	expected := map[time.Duration]struct{}{
		59 * time.Second: {},
		61 * time.Second: {},
		67 * time.Second: {},
		71 * time.Second: {},
		73 * time.Second: {},
	}
	if len(primes.Seconds) != len(expected) {
		t.Fatalf("Seconds size=%d want=%d", len(primes.Seconds), len(expected))
	}
	for _, p := range primes.Seconds {
		if _, ok := expected[p]; !ok {
			t.Errorf("unexpected value %s in Seconds", p)
		}
	}
}

func TestHours_OnlyContainsExpectedPrimes(t *testing.T) {
	expected := map[time.Duration]struct{}{
		3557 * time.Second: {},
		3571 * time.Second: {},
		3593 * time.Second: {},
		3607 * time.Second: {},
		3613 * time.Second: {},
	}
	if len(primes.Hours) != len(expected) {
		t.Fatalf("Hours size=%d want=%d", len(primes.Hours), len(expected))
	}
	for _, p := range primes.Hours {
		if _, ok := expected[p]; !ok {
			t.Errorf("unexpected value %s in Hours", p)
		}
	}
}

func TestPick_CoversWholePool(t *testing.T) {
	seen := make(map[time.Duration]int)
	for i := 0; i < 1000; i++ {
		seen[primes.Pick(primes.Seconds)]++
	}
	if len(seen) != len(primes.Seconds) {
		t.Fatalf("Pick hit %d distinct values in 1000 calls; want %d", len(seen), len(primes.Seconds))
	}
	for _, p := range primes.Seconds {
		if seen[p] == 0 {
			t.Errorf("Seconds prime %s never selected by Pick", p)
		}
	}
}

func TestPick_CustomPool(t *testing.T) {
	custom := []time.Duration{79 * time.Second, 83 * time.Second}
	hit := make(map[time.Duration]bool)
	for i := 0; i < 500; i++ {
		hit[primes.Pick(custom)] = true
	}
	if !hit[79*time.Second] || !hit[83*time.Second] {
		t.Fatalf("Pick did not cover custom pool: %v", hit)
	}
}
