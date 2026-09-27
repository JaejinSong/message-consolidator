package services

import "testing"

func TestCountTokenHits(t *testing.T) {
	tests := []struct {
		name     string
		tokens   []string
		haystack string
		want     int
	}{
		{
			name:     "all tokens present, case-insensitive",
			tokens:   []string{"Indofood", "PO"},
			haystack: "indofood po shipment update",
			want:     2,
		},
		{
			name:     "no tokens present",
			tokens:   []string{"samco", "invoice"},
			haystack: "indofood po shipment update",
			want:     0,
		},
		{
			name:     "partial overlap",
			tokens:   []string{"indofood", "samco"},
			haystack: "Indofood PO shipment",
			want:     1,
		},
		{
			name:     "empty tokens",
			tokens:   nil,
			haystack: "anything",
			want:     0,
		},
		{
			name:     "empty haystack",
			tokens:   []string{"foo"},
			haystack: "",
			want:     0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := countTokenHits(tc.tokens, tc.haystack)
			if got != tc.want {
				t.Errorf("countTokenHits(%v, %q) = %d, want %d", tc.tokens, tc.haystack, got, tc.want)
			}
		})
	}
}
