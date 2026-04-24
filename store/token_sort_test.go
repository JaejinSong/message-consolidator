package store

import (
	"testing"
)

func TestSortedNameTokens(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"phathit chulothok", "chulothok phathit"},
		{"chulothok phathit", "chulothok phathit"}, // already sorted — no change
		{"jaejin song", "jaejin song"},              // already sorted
		{"song jaejin", "jaejin song"},
		{"user@example.com", "user@example.com"}, // email unchanged
		{"+82101234", "+82101234"},                // phone unchanged
		{"solo", "solo"},                          // single token unchanged
		{"", ""},
	}
	for _, c := range cases {
		if got := sortedNameTokens(c.in); got != c.want {
			t.Errorf("sortedNameTokens(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
