package store

import (
	"testing"
	"time"
)

func TestMessageMetadataRoundTripPreservesUnknownKeys(t *testing.T) {
	// A foreign key written by another feature must survive parse → Set → Marshal.
	raw := `{"foreign_feature":{"nested":[1,2,3],"flag":true},"reminded_at_24h":"2026-07-01T00:00:00Z"}`
	md := ParseMetadata(raw)
	if err := md.Set(metaKeyReminded("1h"), "2026-07-22T09:00:00Z"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	out, err := md.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got := ParseMetadata(out)
	if !got.Has("foreign_feature") {
		t.Fatal("foreign key dropped by round-trip")
	}
	var foreign struct {
		Nested []int `json:"nested"`
		Flag   bool  `json:"flag"`
	}
	if !got.Decode("foreign_feature", &foreign) || len(foreign.Nested) != 3 || !foreign.Flag {
		t.Fatalf("foreign key mutated by round-trip: %s", out)
	}
	if got.String(metaKeyReminded("24h")) != "2026-07-01T00:00:00Z" {
		t.Fatal("existing key lost")
	}
	if got.String(metaKeyReminded("1h")) != "2026-07-22T09:00:00Z" {
		t.Fatal("Set key missing after round-trip")
	}
}

func TestParseMetadataDegradesGracefully(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"malformed", `{"broken":`},
		{"non-object", `[1,2,3]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := ParseMetadata(tt.raw)
			if md.Has("anything") {
				t.Fatal("expected empty metadata set")
			}
			if md.String("anything") != "" {
				t.Fatal("expected empty string for absent key")
			}
			if _, ok := md.Time("anything"); ok {
				t.Fatal("expected no time for absent key")
			}
			// Set must still work on a degraded parse.
			if err := md.Set("k", "v"); err != nil {
				t.Fatalf("Set on empty metadata: %v", err)
			}
			if md.String("k") != "v" {
				t.Fatal("Set/String round-trip failed")
			}
		})
	}
}

func TestMessageMetadataTypedGetters(t *testing.T) {
	raw := `{"ts":"2026-07-22T09:00:00Z","not_time":"soon","num":5,"cand":{"status":"pending","confidence":0.9}}`
	md := ParseMetadata(raw)

	ts, ok := md.Time("ts")
	if !ok || !ts.Equal(time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("Time: got %v %v", ts, ok)
	}
	if _, ok := md.Time("not_time"); ok {
		t.Fatal("Time must reject non-RFC3339 strings")
	}
	if md.String("num") != "" {
		t.Fatal("String must return empty for non-string values")
	}

	var cand CompletionCandidate
	if !md.Decode("cand", &cand) || cand.Status != "pending" || cand.Confidence != 0.9 {
		t.Fatalf("Decode: %+v", cand)
	}
	if md.Decode("missing", &cand) {
		t.Fatal("Decode must report false for absent key")
	}
}
