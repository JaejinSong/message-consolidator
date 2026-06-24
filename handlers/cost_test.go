package handlers

import (
	"math"
	"message-consolidator/store"
	"testing"
)

func TestRateFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		model         string
		wantInputPerM float64
		wantOutputPerM float64
	}{
		{"deepseek-chat", 0.14, 0.28},
		{"deepseek-reasoner", 0.14, 0.28},
		{"deepseek-v4-pro", 0.435, 0.87},
		{"deepseek-chat-20260101", 0.14, 0.28}, // versioned suffix → prefix match
		{"gemini-3-flash-preview", 0.50, 3.00},
		{"gemini-3.1-flash-lite", 0.50, 3.00},  // no exact/prefix row → conservative Flash fallback
		{"totally-unknown-model", 0.50, 3.00},  // fallback
	}
	for _, tc := range cases {
		r := rateFor(tc.model)
		if r.InputPerM != tc.wantInputPerM || r.OutputPerM != tc.wantOutputPerM {
			t.Errorf("rateFor(%q) = in %.4f/out %.4f, want in %.4f/out %.4f", tc.model, r.InputPerM, r.OutputPerM, tc.wantInputPerM, tc.wantOutputPerM)
		}
	}
}

func TestCostByModel(t *testing.T) {
	t.Parallel()
	models := []store.ModelTokenUsage{
		{Model: "deepseek-chat", Prompt: 1_000_000, Completion: 1_000_000, Thinking: 1_000_000},
		{Model: "gemini-3-flash-preview", Prompt: 1_000_000, Completion: 0, Thinking: 0},
	}
	in, out, think := costByModel(models)

	// deepseek-chat: in 0.14 + gemini: in 0.50 = 0.64; out 0.28; think 0.28
	assertFloat(t, "input", in, 0.64)
	assertFloat(t, "output", out, 0.28)
	assertFloat(t, "thinking", think, 0.28)
}

func TestCostByModel_CachedDiscount(t *testing.T) {
	t.Parallel()
	// 1M deepseek-chat prompt tokens, half served from cache: cached 500k @ 0.0028,
	// uncached 500k @ 0.14. Cache lever is the whole point of the cached_tokens column.
	models := []store.ModelTokenUsage{
		{Model: "deepseek-chat", Prompt: 1_000_000, Cached: 500_000},
	}
	in, _, _ := costByModel(models)
	want := (500_000*0.14 + 500_000*0.0028) / 1_000_000
	assertFloat(t, "cached-split input", in, want)

	// Guard: cached > prompt must clamp (never over-discount below the cached rate).
	clamped := []store.ModelTokenUsage{{Model: "deepseek-chat", Prompt: 100, Cached: 999}}
	cin, _, _ := costByModel(clamped)
	assertFloat(t, "clamped input", cin, 100*0.0028/1_000_000)
}

func TestProviderDisplayName(t *testing.T) {
	t.Parallel()
	if got := providerDisplayName("deepseek"); got != "DeepSeek" {
		t.Errorf("deepseek -> %q", got)
	}
	if got := providerDisplayName("DeepSeek"); got != "DeepSeek" {
		t.Errorf("case-insensitive deepseek -> %q", got)
	}
	if got := providerDisplayName(""); got != "Gemini 3 Flash" {
		t.Errorf("empty -> %q, want Gemini 3 Flash", got)
	}
	if got := providerDisplayName("gemini"); got != "Gemini 3 Flash" {
		t.Errorf("gemini -> %q", got)
	}
}

func assertFloat(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s cost = %.9f, want %.9f", label, got, want)
	}
}
