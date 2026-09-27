package services

import (
	"context"
	"math"
	"message-consolidator/internal/testutil"
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
		{"deepseek-v4.1-flash", 0.15, 0.60},    // own row: the dot breaks the v4-flash prefix match
		{"deepseek-chat-20260101", 0.14, 0.28}, // versioned suffix → prefix match
		{"gemini-3-flash-preview", 0.50, 3.00},
		{"gemini-3.1-flash-lite", 0.50, 3.00}, // no exact/prefix row → conservative Flash fallback
		{"totally-unknown-model", 0.50, 3.00}, // fallback
	}
	for _, tc := range cases {
		r := RateFor(tc.model)
		if r.InputPerM != tc.wantInputPerM || r.OutputPerM != tc.wantOutputPerM {
			t.Errorf("RateFor(%q) = in %.4f/out %.4f, want in %.4f/out %.4f", tc.model, r.InputPerM, r.OutputPerM, tc.wantInputPerM, tc.wantOutputPerM)
		}
	}
}

func TestCostByModel(t *testing.T) {
	t.Parallel()
	models := []store.ModelTokenUsage{
		{Model: "deepseek-chat", Prompt: 1_000_000, Completion: 1_000_000, Thinking: 1_000_000},
		{Model: "gemini-3-flash-preview", Prompt: 1_000_000, Completion: 0, Thinking: 0},
	}
	in, out, think := CostByModel(models)

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
	in, _, _ := CostByModel(models)
	want := (500_000*0.14 + 500_000*0.0028) / 1_000_000
	assertFloat(t, "cached-split input", in, want)

	// Guard: cached > prompt must clamp (never over-discount below the cached rate).
	clamped := []store.ModelTokenUsage{{Model: "deepseek-chat", Prompt: 100, Cached: 999}}
	cin, _, _ := CostByModel(clamped)
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
	offIn, offOut, offThink := CostByModel(offPeak)
	peakIn, peakOut, peakThink := CostByModel(peak)

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
	in, _, _ := CostByModel(models)
	want := (500_000*0.66*2 + 500_000*0.022*2) / 1_000_000
	assertFloat(t, "peak cached-split input", in, want)
}

func TestCostByModel_PeakFlagIgnoredWithoutMultiplier(t *testing.T) {
	t.Parallel()
	// Gemini has no peak pricing: a peak-flagged row must cost the same as an off-peak one.
	peak := []store.ModelTokenUsage{{Model: "gemini-3-flash-preview", Peak: true, Prompt: 1_000_000}}
	off := []store.ModelTokenUsage{{Model: "gemini-3-flash-preview", Prompt: 1_000_000}}
	peakIn, _, _ := CostByModel(peak)
	offIn, _, _ := CostByModel(off)
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
	in, _, _ := CostByModel(models)
	assertFloat(t, "split-window input", in, 0.22+0.44)
}

func TestProviderDisplayName(t *testing.T) {
	t.Parallel()
	if got := ProviderDisplayName("deepseek"); got != "DeepSeek" {
		t.Errorf("deepseek -> %q", got)
	}
	if got := ProviderDisplayName("DeepSeek"); got != "DeepSeek" {
		t.Errorf("case-insensitive deepseek -> %q", got)
	}
	if got := ProviderDisplayName(""); got != "Gemini 3 Flash" {
		t.Errorf("empty -> %q, want Gemini 3 Flash", got)
	}
	if got := ProviderDisplayName("gemini"); got != "Gemini 3 Flash" {
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
			rate, cached, eligible := CacheHitRate(tc.models)
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

func TestCostsByProvider(t *testing.T) {
	near := func(a, b float64) bool {
		d := a - b
		if d < 0 {
			d = -d
		}
		return d < 1e-9
	}
	// 1M-token rows so cost == rate (cost = tokens / 1e6 * rate).
	models := []store.ModelTokenUsage{
		{Model: "deepseek-chat", Prompt: 1_000_000, Completion: 1_000_000},          // 0.14 in + 0.28 out
		{Model: "deepseek-v4-pro", Completion: 1_000_000},                           // 1.98 out (off-peak)
		{Model: "deepseek-v4-flash", Peak: true, Completion: 1_000_000},             // 0.66 x 2 = 1.32 out
		{Model: "gemini-3-flash-preview", Prompt: 1_000_000, Completion: 1_000_000}, // 0.50 in + 3.00 out
	}
	got := CostsByProvider(models)

	if len(got) != 2 {
		t.Fatalf("expected 2 providers, got %d: %+v", len(got), got)
	}
	if got[0].Provider != "DeepSeek" || got[1].Provider != "Gemini" {
		t.Fatalf("expected fixed order [DeepSeek, Gemini], got [%s, %s]", got[0].Provider, got[1].Provider)
	}

	ds := got[0]
	if ds.Prompt != 1_000_000 || ds.Completion != 3_000_000 {
		t.Errorf("DeepSeek tokens: prompt=%d completion=%d, want 1000000/3000000", ds.Prompt, ds.Completion)
	}
	if wantDS := 0.14 + 0.28 + 1.98 + 1.32; !near(ds.Cost, wantDS) {
		t.Errorf("DeepSeek cost = %f, want %f", ds.Cost, wantDS)
	}

	gm := got[1]
	if wantGM := 0.50 + 3.00; !near(gm.Cost, wantGM) {
		t.Errorf("Gemini cost = %f, want %f", gm.Cost, wantGM)
	}
}

func TestCostsByProviderEmpty(t *testing.T) {
	if got := CostsByProvider(nil); len(got) != 0 {
		t.Errorf("expected no rows for empty usage, got %+v", got)
	}
}

func TestGatherTokenUsageStats(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "tokentest@example.com"

	got, err := GatherTokenUsageStats(context.Background(), email, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TodayPrompt != 0 || got.TodayCompletion != 0 || got.TodayFiltered != 0 ||
		got.TodayTotal != 0 || got.MonthlyPrompt != 0 || got.MonthlyCompletion != 0 ||
		got.MonthlyFiltered != 0 || got.MonthlyTotal != 0 {
		t.Errorf("expected all zero counts for new user, got %+v", got)
	}
	if got.TodayCost != 0.0 || got.MonthlyCost != 0.0 {
		t.Errorf("expected zero costs, got today=%f monthly=%f", got.TodayCost, got.MonthlyCost)
	}
	if got.Model != "Gemini 3 Flash" {
		t.Errorf("expected Model=Gemini 3 Flash, got %q", got.Model)
	}
}

// TestGatherTokenUsageStatsByProvider verifies the end-to-end path: a mixed
// Gemini+DeepSeek month seeded in token_usage flows through GetMonthlyTokenUsageByModel
// and CostsByProvider into the response's MonthlyByProvider split, and the per-provider
// costs sum to the aggregate MonthlyCost.
func TestGatherTokenUsageStatsByProvider(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup DB: %v", err)
	}
	defer cleanup()

	near := func(a, b float64) bool {
		d := a - b
		if d < 0 {
			d = -d
		}
		return d < 1e-9
	}

	email := "byprovider@example.com"
	if _, err := store.GetOrCreateUser(context.Background(), email, "", ""); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// 1M-token rows so each cost component equals its rate.
	if err := store.AddTokenUsage(email, "Analyze", "deepseek-chat", "slack", 0, 1_000_000, 1_000_000, 0, 0); err != nil {
		t.Fatalf("seed deepseek: %v", err)
	}
	if err := store.AddTokenUsage(email, "Analyze", "gemini-3-flash-preview", "slack", 0, 1_000_000, 1_000_000, 0, 0); err != nil {
		t.Fatalf("seed gemini: %v", err)
	}

	got, err := GatherTokenUsageStats(context.Background(), email, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.MonthlyByProvider) != 2 {
		t.Fatalf("expected 2 providers, got %d: %+v", len(got.MonthlyByProvider), got.MonthlyByProvider)
	}
	ds, gm := got.MonthlyByProvider[0], got.MonthlyByProvider[1]
	if ds.Provider != "DeepSeek" || gm.Provider != "Gemini" {
		t.Fatalf("expected order [DeepSeek, Gemini], got [%s, %s]", ds.Provider, gm.Provider)
	}
	if !near(ds.Cost, 0.14+0.28) {
		t.Errorf("DeepSeek cost = %f, want %f", ds.Cost, 0.14+0.28)
	}
	if !near(gm.Cost, 0.50+3.00) {
		t.Errorf("Gemini cost = %f, want %f", gm.Cost, 0.50+3.00)
	}
	if !near(got.MonthlyCost, ds.Cost+gm.Cost) {
		t.Errorf("MonthlyCost %f != sum of per-provider %f", got.MonthlyCost, ds.Cost+gm.Cost)
	}
}
