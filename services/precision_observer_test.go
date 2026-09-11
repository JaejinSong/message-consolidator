package services

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"message-consolidator/db"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func TestPrecisionBucketQualifies(t *testing.T) {
	cases := []struct {
		name     string
		resolved int64
		canceled int64
		want     bool
	}{
		{"below volume floor", 6, 6, false},
		{"at volume floor and fully cancelled", 7, 7, true},
		{"at rate floor", 10, 7, true},
		{"just under rate floor", 10, 6, false},
		{"high volume, average rate", 100, 40, false},
		{"nothing resolved", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := precisionBucket{resolved: tc.resolved, canceled: tc.canceled}
			if got := b.qualifies(); got != tc.want {
				t.Errorf("qualifies() = %v (rate %.2f), want %v", got, b.cancelRate(), tc.want)
			}
		})
	}
}

func TestSplitConcatIDs(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int
	}{
		{"string", "11,12,13", 3},
		{"bytes", []byte("11,12"), 2},
		{"empty string", "", 0},
		{"whitespace", "   ", 0},
		{"nil", nil, 0},
		{"trailing separator", "11,12,", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := splitConcatIDs(tc.in); len(got) != tc.want {
				t.Errorf("splitConcatIDs(%v) = %v, want %d ids", tc.in, got, tc.want)
			}
		})
	}
}

func TestNullFloatToInt(t *testing.T) {
	if got := nullFloatToInt(sql.NullFloat64{}); got != 0 {
		t.Errorf("invalid = %d, want 0", got)
	}
	if got := nullFloatToInt(sql.NullFloat64{Float64: 17, Valid: true}); got != 17 {
		t.Errorf("valid = %d, want 17", got)
	}
}

// TestObservePrecision_ProposesAndStaysInert is the main gate. It covers the threshold,
// idempotency across runs, and -- most importantly -- the structural guarantee that a
// proposal can never be applied: ListActiveSuppressRules filters kind='suppress', and
// these rows are kind='precision'. An auto-applied suppression would be unfalsifiable,
// since the engine would stop producing the evidence that could overturn it.
func TestObservePrecision_ProposesAndStaysInert(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant := testutil.RandomEmail("tenant")
	conn := store.GetDB()

	seed := func(room, assignee, task string, done, deleted int) {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO messages (user_email, source, room, task, assignee, done, is_deleted, created_at)
			 VALUES (?, 'whatsapp', ?, ?, ?, ?, ?, datetime('now','-1 day'))`,
			tenant, room, task, assignee, done, deleted); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// A bad room: 9 cancelled, 1 completed -> 90%, clears both floors.
	for i := 0; i < 9; i++ {
		seed("Bad Room", "shared", fmt.Sprintf("Review thing %d", i), 0, 1)
	}
	seed("Bad Room", "shared", "Review the kept thing", 1, 0)
	// A healthy room: 8 completed, 1 cancelled -> 11%, must not be proposed.
	for i := 0; i < 8; i++ {
		seed("Good Room", "shared", fmt.Sprintf("Attend meeting %d", i), 1, 0)
	}
	seed("Good Room", "shared", "Attend the cancelled one", 0, 1)
	// Too few to judge: 3 cancelled in a third room.
	for i := 0; i < 3; i++ {
		seed("Thin Room", "shared", fmt.Sprintf("Check item %d", i), 0, 1)
	}

	n, err := ObservePrecision(ctx, tenant)
	if err != nil {
		t.Fatalf("ObservePrecision: %v", err)
	}
	if n == 0 {
		t.Fatal("no bucket proposed; the 90% room should qualify")
	}

	q := db.New(conn)
	props, err := q.ListPrecisionObservations(ctx, tenant)
	if err != nil {
		t.Fatalf("ListPrecisionObservations: %v", err)
	}
	byScope := map[string]int64{}
	for _, p := range props {
		if p.Status != "pending" {
			t.Errorf("proposal %s/%s has status %q; proposals must stay pending", p.Scope, p.FromValue, p.Status)
		}
		byScope[p.Scope+" "+p.FromValue] = p.EvidenceCount
	}
	if got, ok := byScope["whatsapp|Bad Room owner=shared"]; !ok || got != 9 {
		t.Errorf("Bad Room proposal = %d (present=%v), want 9 cancellations", got, ok)
	}
	if _, ok := byScope["whatsapp|Good Room owner=shared"]; ok {
		t.Error("Good Room was proposed; an 11% cancel rate must not qualify")
	}
	if _, ok := byScope["whatsapp|Thin Room owner=shared"]; ok {
		t.Error("Thin Room was proposed; 3 resolved tasks are below the volume floor")
	}

	// Why: the observer runs hourly, so a second pass must update the same row rather than
	// accumulate duplicates or inflate the count.
	before := len(props)
	if _, err := ObservePrecision(ctx, tenant); err != nil {
		t.Fatalf("second ObservePrecision: %v", err)
	}
	after, err := q.ListPrecisionObservations(ctx, tenant)
	if err != nil {
		t.Fatalf("relist: %v", err)
	}
	if len(after) != before {
		t.Errorf("proposal count moved from %d to %d across runs; upsert is not idempotent", before, len(after))
	}
	for _, p := range after {
		if p.Scope == "whatsapp|Bad Room" && p.FromValue == "owner=shared" && p.EvidenceCount != 9 {
			t.Errorf("evidence inflated to %d on re-run; it must reflect the measurement, not accumulate", p.EvidenceCount)
		}
	}

	// The guarantee: nothing here is reachable by the suppression actuator.
	rules, err := q.ListActiveSuppressRules(ctx, tenant)
	if err != nil {
		t.Fatalf("ListActiveSuppressRules: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("%d precision proposal(s) leaked into the suppress actuator: %+v", len(rules), rules)
	}
}
