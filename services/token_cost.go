package services

import (
	"context"
	"errors"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
)

// TokenUnitDenominator converts raw token counts to per-million pricing.
const TokenUnitDenominator = 1000000.0

// ModelRate is the per-1M-token off-peak price for a model. CachedInputPerM is the
// discounted prompt-cache-hit rate applied in CostByModel; a zero value means the model does
// not participate in prompt caching (e.g. Gemini). PeakMultiplier scales every component
// during the provider's peak-rate window; zero means the model bills at one flat rate.
type ModelRate struct {
	InputPerM       float64
	CachedInputPerM float64
	OutputPerM      float64
	ThinkingPerM    float64
	PeakMultiplier  float64
}

// deepSeekPeakMultiplier is DeepSeek's peak-window surcharge: every component bills at twice
// the off-peak rate inside the windows store.isPeakWindow marks on each usage row.
const deepSeekPeakMultiplier = 2.0

// inWindow returns the rate the tokens actually billed at: the base off-peak rate, or every
// component scaled by PeakMultiplier when they were consumed in the provider's peak window.
func (r ModelRate) inWindow(peak bool) ModelRate {
	if !peak || r.PeakMultiplier == 0 {
		return r
	}
	return ModelRate{
		InputPerM:       r.InputPerM * r.PeakMultiplier,
		CachedInputPerM: r.CachedInputPerM * r.PeakMultiplier,
		OutputPerM:      r.OutputPerM * r.PeakMultiplier,
		ThinkingPerM:    r.ThinkingPerM * r.PeakMultiplier,
		PeakMultiplier:  r.PeakMultiplier,
	}
}

// aiRates prices each model at its own published rate so model-mixed history (Gemini +
// DeepSeek rows) is billed correctly. Keys are mutually non-prefixing; unknown/legacy ids
// fall back to the Gemini 3 Flash rate (conservative upper bound) via RateFor.
// DeepSeek V4 rows carry the off-peak base rate published 2026-08-16 and a 2x peak
// multiplier, applied per row by inWindow. The v3 ids keep their pre-migration flat rates
// so historical token_usage rows (all peak=0 after the v17 backfill) stay billed at what
// they actually cost. glm-5.3-flash (report stage) carries Z.ai's published list rate and no
// peak multiplier - it bills one flat rate, so a launch-promotion window would show as
// overstated spend rather than a missing charge.
var aiRates = map[string]ModelRate{
	"deepseek-chat":     {InputPerM: 0.14, CachedInputPerM: 0.0028, OutputPerM: 0.28, ThinkingPerM: 0.28},
	"deepseek-reasoner": {InputPerM: 0.14, CachedInputPerM: 0.0028, OutputPerM: 0.28, ThinkingPerM: 0.28},
	"deepseek-v4-flash": {InputPerM: 0.22, CachedInputPerM: 0.007, OutputPerM: 0.66, ThinkingPerM: 0.66, PeakMultiplier: deepSeekPeakMultiplier},
	// Why: "deepseek-v4.1-flash" is not a prefix of "deepseek-v4-flash", so without its own
	// key RateFor would silently fall through to the Gemini rate (4.5x the real output cost).
	"deepseek-v4.1-flash":    {InputPerM: 0.15, CachedInputPerM: 0.003, OutputPerM: 0.60, ThinkingPerM: 0.60, PeakMultiplier: deepSeekPeakMultiplier},
	"deepseek-v4-pro":        {InputPerM: 0.66, CachedInputPerM: 0.022, OutputPerM: 1.98, ThinkingPerM: 1.98, PeakMultiplier: deepSeekPeakMultiplier},
	"gemini-3-flash-preview": {InputPerM: 0.50, OutputPerM: 3.00, ThinkingPerM: 3.00},
	"glm-5.3-flash":          {InputPerM: 0.15, CachedInputPerM: 0.03, OutputPerM: 0.50, ThinkingPerM: 0.50},
}

// RateFor resolves a model id to its rate: exact match, then prefix match (versioned ids),
// then a conservative Gemini-3-Flash fallback. gemini-3.1-flash-lite intentionally uses the
// fallback pending authoritative lite pricing (the prior dashboard also priced it at Flash).
func RateFor(model string) ModelRate {
	if r, ok := aiRates[model]; ok {
		return r
	}
	for prefix, r := range aiRates {
		if strings.HasPrefix(model, prefix) {
			return r
		}
	}
	return aiRates["gemini-3-flash-preview"]
}

// ProviderDisplayName labels the cost dashboard by the active provider. The displayed
// label is approximate when history spans both providers; per-model rows drive the cost.
func ProviderDisplayName(provider string) string {
	if strings.EqualFold(provider, "deepseek") {
		return "DeepSeek"
	}
	return "Gemini 3 Flash"
}

// CostByModel prices each row's tokens at its own rate and returns the input/output/thinking
// USD components summed across rows (already divided by the per-million denominator).
// Rows are per (model, peak window), so peak-window tokens pick up the model's multiplier.
// Cached tokens are a subset of prompt tokens (DeepSeek prompt-cache hits) billed at the
// discounted CachedInputPerM rate; the remainder is billed at the full InputPerM rate.
func CostByModel(models []store.ModelTokenUsage) (input, output, thinking float64) {
	for _, m := range models {
		r := RateFor(m.Model).inWindow(m.Peak)
		cached := m.Cached
		if cached > m.Prompt {
			cached = m.Prompt // guard: cached is a subset of prompt; never over-discount
		}
		uncached := m.Prompt - cached
		input += float64(uncached)*r.InputPerM + float64(cached)*r.CachedInputPerM
		output += float64(m.Completion) * r.OutputPerM
		thinking += float64(m.Thinking) * r.ThinkingPerM
	}
	return input / TokenUnitDenominator, output / TokenUnitDenominator, thinking / TokenUnitDenominator
}

// providerOf groups a model id under its billing provider for the cost breakdown.
func providerOf(model string) string {
	if strings.HasPrefix(model, "deepseek") {
		return "DeepSeek"
	}
	return "Gemini"
}

// ProviderCost is one row of the per-provider monthly cost breakdown, so the dashboard
// can show Gemini vs DeepSeek separately even across a mixed-history month.
type ProviderCost struct {
	Provider     string  `json:"provider"`
	Prompt       int     `json:"prompt"`
	Completion   int     `json:"completion"`
	Thinking     int     `json:"thinking"`
	Cached       int     `json:"cached"`
	Cost         float64 `json:"cost"`
	CostInput    float64 `json:"costInput"`
	CostOutput   float64 `json:"costOutput"`
	CostThinking float64 `json:"costThinking"`
}

// CostsByProvider groups per-model usage under its provider and prices each group with the
// same per-model rates as CostByModel. Fixed order (DeepSeek, Gemini); providers with no
// usage are omitted so the UI only renders rows that have data.
func CostsByProvider(models []store.ModelTokenUsage) []ProviderCost {
	groups := map[string][]store.ModelTokenUsage{}
	for _, m := range models {
		p := providerOf(m.Model)
		groups[p] = append(groups[p], m)
	}
	out := make([]ProviderCost, 0, len(groups))
	for _, p := range []string{"DeepSeek", "Gemini"} {
		ms, ok := groups[p]
		if !ok {
			continue
		}
		in, outc, think := CostByModel(ms)
		pc := ProviderCost{Provider: p, CostInput: in, CostOutput: outc, CostThinking: think, Cost: in + outc + think}
		for _, m := range ms {
			pc.Prompt += m.Prompt
			pc.Completion += m.Completion
			pc.Thinking += m.Thinking
			pc.Cached += m.Cached
		}
		out = append(out, pc)
	}
	return out
}

// TokenUsageResponse is the daily/monthly AI token usage and cost payload rendered by the
// user info and token-usage dashboard endpoints.
type TokenUsageResponse struct {
	TodayPrompt         int            `json:"todayPrompt"`
	TodayCompletion     int            `json:"todayCompletion"`
	TodayThinking       int            `json:"todayThinking"`
	TodayFiltered       int            `json:"todayFiltered"`
	TodayTotal          int            `json:"todayTotal"`
	TodayCost           float64        `json:"todayCost"`
	MonthlyPrompt       int            `json:"monthlyPrompt"`
	MonthlyCompletion   int            `json:"monthlyCompletion"`
	MonthlyThinking     int            `json:"monthlyThinking"`
	MonthlyFiltered     int            `json:"monthlyFiltered"`
	MonthlyTotal        int            `json:"monthlyTotal"`
	MonthlyCached       int            `json:"monthlyCached"`
	MonthlyCacheHitRate float64        `json:"monthlyCacheHitRate"` // cached / prompt input tokens (0..1)
	MonthlyCost         float64        `json:"monthlyCost"`
	MonthlyCostInput    float64        `json:"monthlyCostInput"`
	MonthlyCostOutput   float64        `json:"monthlyCostOutput"`
	MonthlyCostThinking float64        `json:"monthlyCostThinking"`
	MonthlyByProvider   []ProviderCost `json:"monthlyByProvider"`
	Model               string         `json:"model"`
}

// CacheHitRate computes cached/cacheEligiblePrompt over the supplied model rows.
// Only models whose rate has CachedInputPerM > 0 contribute to the denominator so that
// Gemini rows (CachedInputPerM == 0) do not dilute DeepSeek's real hit rate.
// Returns (hitRate, totalCached, cacheEligiblePromptTokens).
func CacheHitRate(models []store.ModelTokenUsage) (rate float64, totalCached, eligiblePrompt int) {
	for _, m := range models {
		totalCached += m.Cached
		if RateFor(m.Model).CachedInputPerM > 0 {
			eligiblePrompt += m.Prompt
		}
	}
	if eligiblePrompt > 0 {
		rate = float64(totalCached) / float64(eligiblePrompt)
	}
	return rate, totalCached, eligiblePrompt
}

// GatherTokenUsageStats includes daily and monthly AI token usage for cost transparency.
// Token counts come from the aggregate daily/monthly queries; cost is priced per-model (aiRates)
// so a Gemini->DeepSeek mixed history is billed at each row's own rate.
// Why: each of the four underlying lookups is logged individually with the failing email so a
// dashboard showing zero can be traced back to the specific query that failed, instead of
// silently looking like a user with no usage. Every failure is joined into the returned error
// so the caller decides whether to fail the request or fall back to the (partial) response.
func GatherTokenUsageStats(ctx context.Context, email, aiProvider string) (TokenUsageResponse, error) {
	todayPrompt, todayCompletion, todayThinking, todayFiltered, todayErr := store.GetDailyTokenUsage(ctx, email)
	if todayErr != nil {
		logger.Errorf("[TOKEN_COST] daily token usage lookup failed for %s: %v", email, todayErr)
	}
	monthPrompt, monthCompletion, monthThinking, monthFiltered, monthErr := store.GetMonthlyTokenUsage(ctx, email)
	if monthErr != nil {
		logger.Errorf("[TOKEN_COST] monthly token usage lookup failed for %s: %v", email, monthErr)
	}
	dailyModels, dailyModelsErr := store.GetDailyTokenUsageByModel(ctx, email)
	if dailyModelsErr != nil {
		logger.Errorf("[TOKEN_COST] daily token usage by model lookup failed for %s: %v", email, dailyModelsErr)
	}
	monthlyModels, monthlyModelsErr := store.GetMonthlyTokenUsageByModel(ctx, email)
	if monthlyModelsErr != nil {
		logger.Errorf("[TOKEN_COST] monthly token usage by model lookup failed for %s: %v", email, monthlyModelsErr)
	}

	dayCostIn, dayCostOut, dayCostThink := CostByModel(dailyModels)
	monthCostIn, monthCostOut, monthCostThink := CostByModel(monthlyModels)
	monthCacheHitRate, monthCached, _ := CacheHitRate(monthlyModels)

	resp := TokenUsageResponse{
		TodayPrompt:         todayPrompt,
		TodayCompletion:     todayCompletion,
		TodayThinking:       todayThinking,
		TodayFiltered:       todayFiltered,
		TodayTotal:          todayPrompt + todayCompletion + todayThinking,
		TodayCost:           dayCostIn + dayCostOut + dayCostThink,
		MonthlyPrompt:       monthPrompt,
		MonthlyCompletion:   monthCompletion,
		MonthlyThinking:     monthThinking,
		MonthlyFiltered:     monthFiltered,
		MonthlyTotal:        monthPrompt + monthCompletion + monthThinking,
		MonthlyCached:       monthCached,
		MonthlyCacheHitRate: monthCacheHitRate,
		MonthlyCost:         monthCostIn + monthCostOut + monthCostThink,
		MonthlyCostInput:    monthCostIn,
		MonthlyCostOutput:   monthCostOut,
		MonthlyCostThinking: monthCostThink,
		MonthlyByProvider:   CostsByProvider(monthlyModels),
		Model:               ProviderDisplayName(aiProvider),
	}

	if err := errors.Join(todayErr, monthErr, dailyModelsErr, monthlyModelsErr); err != nil {
		return resp, fmt.Errorf("gather token usage stats for %s: %w", email, err)
	}
	return resp, nil
}
