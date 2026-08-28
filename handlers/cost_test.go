package handlers

import (
	"math"
	"message-consolidator/store"
	"testing"
)

func TestRateFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		model          string
		wantInputPerM  float64
		wantOutputPerM float64
	}{
		{"deepseek-chat", 0.14, 0.28},
		{"deepseek-reasoner", 0.14, 0.28},
		{"deepseek-v4-pro", 0.66, 1.98},
		{"deepseek-v4-flash", 0.22, 0.66},
		{"deepseek-v4-flash:0731", 0.22, 0.66}, // Ollama tag suffix -> prefix match
		{"deepseek-chat-20260101", 0.14, 0.28}, // versioned suffix → prefix match
		{"gemini-3-flash-preview", 0.50, 3.00},
		{"gemini-3.1-flash-lite", 0.50, 3.00}, // no exact/prefix row → conservative Flash fallback
		{"totally-unknown-model", 0.50, 3.00}, // fallback
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

func TestCostByModel_PeakWindowDoublesRate(t *testing.T) {
	t.Parallel()
	// Same token counts in each window: the peak row must bill at exactly 2x the off-peak row.
	offPeak := []store.ModelTokenUsage{
		{Model: "deepseek-v4-flash", Prompt: 1_000_000, Completion: 1_000_000, Thinking: 1_000_000},
	}
	peak := []store.ModelTokenUsage{
		{Model: "deepseek-v4-flash", Peak: true, Prompt: 1_000_000, Completion: 1_000_000, Thinking: 1_000_000},
	}
	offIn, offOut, offThink := costByModel(offPeak)
	peakIn, peakOut, peakThink := costByModel(peak)

	assertFloat(t, "off-peak input", offIn, 0.22)
	assertFloat(t, "peak input", peakIn, 0.44)
	assertFloat(t, "off-peak output", offOut, 0.66)
	assertFloat(t, "peak output", peakOut, 1.32)
	assertFloat(t, "off-peak thinking", offThink, 0.66)
	assertFloat(t, "peak thinking", peakThink, 1.32)
}

func TestCostByModel_PeakCachedRateAlsoDoubles(t *testing.T) {
	t.Parallel()
	// The cache-hit rate is multiplied too, so a cached-heavy peak row is not under-billed.
	models := []store.ModelTokenUsage{
		{Model: "deepseek-v4-pro", Peak: true, Prompt: 1_000_000, Cached: 500_000},
	}
	in, _, _ := costByModel(models)
	want := (500_000*0.66*2 + 500_000*0.022*2) / 1_000_000
	assertFloat(t, "peak cached-split input", in, want)
}

func TestCostByModel_PeakFlagIgnoredWithoutMultiplier(t *testing.T) {
	t.Parallel()
	// Gemini has no peak pricing: a peak-flagged row must cost the same as an off-peak one.
	peak := []store.ModelTokenUsage{{Model: "gemini-3-flash-preview", Peak: true, Prompt: 1_000_000}}
	off := []store.ModelTokenUsage{{Model: "gemini-3-flash-preview", Prompt: 1_000_000}}
	peakIn, _, _ := costByModel(peak)
	offIn, _, _ := costByModel(off)
	assertFloat(t, "gemini peak input", peakIn, offIn)
	assertFloat(t, "gemini peak input", peakIn, 0.50)
}

func TestCostByModel_SplitWindowsSumPerRow(t *testing.T) {
	t.Parallel()
	// One model spanning both windows arrives as two rows; each must price at its own rate.
	models := []store.ModelTokenUsage{
		{Model: "deepseek-v4-flash", Prompt: 1_000_000},
		{Model: "deepseek-v4-flash", Peak: true, Prompt: 1_000_000},
	}
	in, _, _ := costByModel(models)
	assertFloat(t, "split-window input", in, 0.22+0.44)
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

func TestCacheHitRate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		models       []store.ModelTokenUsage
		wantRate     float64
		wantCached   int
		wantEligible int
	}{
		{
			name: "deepseek only — rate equals cached/prompt",
			models: []store.ModelTokenUsage{
				{Model: "deepseek-chat", Prompt: 100_000, Cached: 80_000},
			},
			wantRate:     0.8,
			wantCached:   80_000,
			wantEligible: 100_000,
		},
		{
			name: "gemini dilution suppressed — denominator excludes gemini prompt",
			models: []store.ModelTokenUsage{
				{Model: "gemini-3-flash-preview", Prompt: 1_000_000, Cached: 0},
				{Model: "deepseek-chat", Prompt: 100_000, Cached: 80_000},
			},
			// rate must equal deepseek-only ratio 80k/100k = 0.8, NOT 80k/1100k ≈ 0.07
			wantRate:     0.8,
			wantCached:   80_000,
			wantEligible: 100_000,
		},
		{
			name:         "no models — zero rate",
			models:       []store.ModelTokenUsage{},
			wantRate:     0.0,
			wantCached:   0,
			wantEligible: 0,
		},
		{
			name: "gemini only — no eligible prompt, rate stays zero",
			models: []store.ModelTokenUsage{
				{Model: "gemini-3-flash-preview", Prompt: 500_000, Cached: 0},
			},
			wantRate:     0.0,
			wantCached:   0,
			wantEligible: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rate, cached, eligible := cacheHitRate(tc.models)
			assertFloat(t, "rate", rate, tc.wantRate)
			if cached != tc.wantCached {
				t.Errorf("cached = %d, want %d", cached, tc.wantCached)
			}
			if eligible != tc.wantEligible {
				t.Errorf("eligible = %d, want %d", eligible, tc.wantEligible)
			}
		})
	}
}
