package types

import "testing"

func TestIsValidTaskCategory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "Task Uppercase", in: "TASK", want: true},
		{name: "Policy Uppercase", in: "POLICY", want: true},
		{name: "Query Uppercase", in: "QUERY", want: true},
		{name: "Promise Uppercase", in: "PROMISE", want: true},
		{name: "Waiting Uppercase", in: "WAITING", want: true},
		{name: "Lowercase Task", in: "task", want: true},
		{name: "Mixed Case Waiting", in: "Waiting", want: true},
		{name: "Unknown Category", in: "NOTES", want: false},
		{name: "Empty String", in: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsValidTaskCategory(tt.in); got != tt.want {
				t.Errorf("IsValidTaskCategory(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
