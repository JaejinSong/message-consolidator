package services

import (
	"context"
	"errors"
	"math"
	"testing"

	"message-consolidator/store"
)

// stubEmbedder is a deterministic Embedder substitute. EmbedDocument hashes the
// input bytes into a stable vector so tests can craft reproducible cosine
// landscapes without touching Gemini.
type stubEmbedder struct {
	dim     int
	docs    map[string][]float32
	queries map[string][]float32
	failOn  string
}

func (s *stubEmbedder) Model() string { return "stub-test" }
func (s *stubEmbedder) Dim() int      { return s.dim }
func (s *stubEmbedder) EmbedDocument(_ context.Context, text string) ([]float32, error) {
	if text == s.failOn {
		return nil, errors.New("stub fail")
	}
	if v, ok := s.docs[text]; ok {
		return v, nil
	}
	return synthVector(s.dim, text), nil
}
func (s *stubEmbedder) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	if v, ok := s.queries[text]; ok {
		return v, nil
	}
	return synthVector(s.dim, text), nil
}

func synthVector(dim int, seed string) []float32 {
	v := make([]float32, dim)
	h := uint32(2166136261)
	for _, b := range []byte(seed) {
		h ^= uint32(b)
		h *= 16777619
	}
	for i := range v {
		h = h*1103515245 + 12345
		v[i] = float32(int32(h)) / float32(math.MaxInt32)
	}
	return v
}

func TestFloat32sBytesRoundTrip(t *testing.T) {
	in := []float32{0, -1.5, 3.14159, 1e-9, -0.0, 1234.5}
	out := BytesToFloat32s(Float32sToBytes(in))
	if len(out) != len(in) {
		t.Fatalf("length mismatch: got %d want %d", len(out), len(in))
	}
	for i := range in {
		if in[i] != out[i] {
			t.Errorf("idx %d: got %v want %v", i, out[i], in[i])
		}
	}
}

func TestBytesToFloat32sBadLength(t *testing.T) {
	// Why: corrupt rows (length not divisible by 4) must return nil so cosineTopK
	// can skip them rather than panic on slice indexing.
	if got := BytesToFloat32s([]byte{1, 2, 3}); got != nil {
		t.Errorf("expected nil for corrupt blob, got %v", got)
	}
}

func TestEmbeddingTextHashChangesWithText(t *testing.T) {
	a := embeddingTextHash("task A", "body 1")
	b := embeddingTextHash("task A", "body 2")
	if a == b {
		t.Errorf("hash should change when original_text changes")
	}
	c := embeddingTextHash("task B", "body 1")
	if a == c {
		t.Errorf("hash should change when task changes")
	}
	// Why: the 0x1F separator prevents (task="ab", original="") and (task="a",
	// original="b") from colliding — verify the boundary is honored.
	d := embeddingTextHash("ab", "")
	e := embeddingTextHash("a", "b")
	if d == e {
		t.Errorf("hash boundary collision: ab/'' must differ from a/b")
	}
}

func TestCosineNormalizedKnownVectors(t *testing.T) {
	cases := []struct {
		name string
		a, b []float32
		want float32
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0},
		{"opposite", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cosineNormalized(tc.a, norm(tc.a), tc.b)
			if math.Abs(float64(got-tc.want)) > 1e-5 {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestRRFFuseCombinesAndOrders(t *testing.T) {
	// Why: id=10 is rank 0 in fts and absent in sem → score = 1/61.
	// id=20 is rank 1 in fts and rank 0 in sem → 1/62 + 1/61 (highest).
	// id=30 absent in fts, rank 1 in sem → 1/62.
	// Expected order: 20, 10, 30.
	fts := []store.MessageID{10, 20}
	sem := []store.MessageID{20, 30}
	got := rrfFuse(fts, sem, 60, 5)
	want := []store.MessageID{20, 10, 30}
	if len(got) != len(want) {
		t.Fatalf("len got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rank %d: got %v want %v (full=%v)", i, got[i], want[i], got)
		}
	}
}

func TestRRFFuseLimitTruncates(t *testing.T) {
	fts := []store.MessageID{1, 2, 3, 4}
	sem := []store.MessageID{5, 6, 7, 8}
	got := rrfFuse(fts, sem, 60, 3)
	if len(got) != 3 {
		t.Errorf("limit=3 → got len=%d", len(got))
	}
}

func TestRRFFuseEmptyInputs(t *testing.T) {
	if got := rrfFuse(nil, nil, 60, 10); len(got) != 0 {
		t.Errorf("expected empty fuse, got %v", got)
	}
}

func TestReorderByIDsPreservesRRFOrder(t *testing.T) {
	msgs := []store.ConsolidatedMessage{
		{ID: 1, Task: "A"},
		{ID: 2, Task: "B"},
		{ID: 3, Task: "C"},
	}
	got := reorderByIDs(msgs, []store.MessageID{3, 1, 2})
	if len(got) != 3 || got[0].ID != 3 || got[1].ID != 1 || got[2].ID != 2 {
		t.Errorf("unexpected order: %+v", got)
	}
}

func TestReorderByIDsSkipsMissing(t *testing.T) {
	msgs := []store.ConsolidatedMessage{{ID: 1}, {ID: 2}}
	got := reorderByIDs(msgs, []store.MessageID{2, 99, 1})
	if len(got) != 2 || got[0].ID != 2 || got[1].ID != 1 {
		t.Errorf("expected [2,1], got %+v", got)
	}
}

func TestNewEmbeddingServiceDefaults(t *testing.T) {
	svc := NewEmbeddingService(&stubEmbedder{dim: 3})
	if svc.pageSize != defaultPageSize {
		t.Errorf("pageSize default wrong: %d", svc.pageSize)
	}
	if svc.rrfK != defaultRRFK {
		t.Errorf("rrfK default wrong: %d", svc.rrfK)
	}
	// RAMBudgetBytes should stay under e2-micro's safety margin (~10 MB).
	if RAMBudgetBytes > 10*1024*1024 {
		t.Errorf("RAM budget %d exceeds 10MB ceiling", RAMBudgetBytes)
	}
}

func TestSearchHybridNilService(t *testing.T) {
	var svc *EmbeddingService
	_, err := svc.SearchHybrid(context.Background(), "x@y", "q", 10)
	if err == nil {
		t.Errorf("nil service must fail")
	}
}

func TestJoinForEmbedding(t *testing.T) {
	if got := joinForEmbedding("t", "o"); got != "t\n\no" {
		t.Errorf("both: got %q", got)
	}
	if got := joinForEmbedding("t", ""); got != "t" {
		t.Errorf("task only: got %q", got)
	}
	if got := joinForEmbedding("", "o"); got != "o" {
		t.Errorf("orig only: got %q", got)
	}
	if got := joinForEmbedding("", ""); got != "" {
		t.Errorf("both empty: got %q", got)
	}
}
