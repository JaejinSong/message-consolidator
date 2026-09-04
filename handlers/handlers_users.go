package handlers

import (
	"context"
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"regexp"
	"strings"
)

// TokenUnitDenominator converts raw token counts to per-million pricing.
const TokenUnitDenominator = 1000000.0

// ModelRate is the per-1M-token off-peak price for a model. CachedInputPerM is the
// discounted prompt-cache-hit rate applied in costByModel; a zero value means the model does
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
// fall back to the Gemini 3 Flash rate (conservative upper bound) via rateFor.
// DeepSeek V4 rows carry the off-peak base rate published 2026-08-16 and a 2x peak
// multiplier, applied per row by inWindow. The v3 ids keep their pre-migration flat rates
// so historical token_usage rows (all peak=0 after the v17 backfill) stay billed at what
// they actually cost. glm-5.3-flash (report stage) carries Z.ai's published list rate and no
// peak multiplier - it bills one flat rate, so a launch-promotion window would show as
// overstated spend rather than a missing charge.
var aiRates = map[string]ModelRate{
	"deepseek-chat":          {InputPerM: 0.14, CachedInputPerM: 0.0028, OutputPerM: 0.28, ThinkingPerM: 0.28},
	"deepseek-reasoner":      {InputPerM: 0.14, CachedInputPerM: 0.0028, OutputPerM: 0.28, ThinkingPerM: 0.28},
	"deepseek-v4-flash":      {InputPerM: 0.22, CachedInputPerM: 0.007, OutputPerM: 0.66, ThinkingPerM: 0.66, PeakMultiplier: deepSeekPeakMultiplier},
	"deepseek-v4-pro":        {InputPerM: 0.66, CachedInputPerM: 0.022, OutputPerM: 1.98, ThinkingPerM: 1.98, PeakMultiplier: deepSeekPeakMultiplier},
	"gemini-3-flash-preview": {InputPerM: 0.50, OutputPerM: 3.00, ThinkingPerM: 3.00},
	"glm-5.3-flash":          {InputPerM: 0.15, CachedInputPerM: 0.03, OutputPerM: 0.50, ThinkingPerM: 0.50},
}

// rateFor resolves a model id to its rate: exact match, then prefix match (versioned ids),
// then a conservative Gemini-3-Flash fallback. gemini-3.1-flash-lite intentionally uses the
// fallback pending authoritative lite pricing (the prior dashboard also priced it at Flash).
func rateFor(model string) ModelRate {
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

// providerDisplayName labels the cost dashboard by the active provider. The displayed
// label is approximate when history spans both providers; per-model rows drive the cost.
func providerDisplayName(provider string) string {
	if strings.EqualFold(provider, "deepseek") {
		return "DeepSeek"
	}
	return "Gemini 3 Flash"
}

// costByModel prices each row's tokens at its own rate and returns the input/output/thinking
// USD components summed across rows (already divided by the per-million denominator).
// Rows are per (model, peak window), so peak-window tokens pick up the model's multiplier.
// Cached tokens are a subset of prompt tokens (DeepSeek prompt-cache hits) billed at the
// discounted CachedInputPerM rate; the remainder is billed at the full InputPerM rate.
func costByModel(models []store.ModelTokenUsage) (input, output, thinking float64) {
	for _, m := range models {
		r := rateFor(m.Model).inWindow(m.Peak)
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

// providerCost is one row of the per-provider monthly cost breakdown, so the dashboard
// can show Gemini vs DeepSeek separately even across a mixed-history month.
type providerCost struct {
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

// costsByProvider groups per-model usage under its provider and prices each group with the
// same per-model rates as costByModel. Fixed order (DeepSeek, Gemini); providers with no
// usage are omitted so the UI only renders rows that have data.
func costsByProvider(models []store.ModelTokenUsage) []providerCost {
	groups := map[string][]store.ModelTokenUsage{}
	for _, m := range models {
		p := providerOf(m.Model)
		groups[p] = append(groups[p], m)
	}
	out := make([]providerCost, 0, len(groups))
	for _, p := range []string{"DeepSeek", "Gemini"} {
		ms, ok := groups[p]
		if !ok {
			continue
		}
		in, outc, think := costByModel(ms)
		pc := providerCost{Provider: p, CostInput: in, CostOutput: outc, CostThinking: think, Cost: in + outc + think}
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

type tokenUsageResponse struct {
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
	MonthlyByProvider   []providerCost `json:"monthlyByProvider"`
	Model               string         `json:"model"`
}

func (a *API) HandleUserInfo(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	logger.Debugf("[USER] fetching info for %s", email)
	user, err := store.GetOrCreateUser(r.Context(), email, "", "")
	if err != nil {
		handleAPIError(w, r, err, "[USER]", "Failed to fetch user info")
		return
	}
	logger.Debugf("[USER] Found user: ID=%d, Email=%s", user.ID, user.Email)

	user.Aliases, _ = store.GetUserAliasesByEmailFromCache(r.Context(), email)
	user.ArchiveDays = store.GetAutoArchiveDays()
	user.StaleThresholdWorkingDays = store.GetStaleThresholdWorkingDays()

	a.autoPopulateSlackAliases(r.Context(), user)
	tokenUsage := a.gatherTokenUsageStats(r.Context(), email)

	// Why: super admin is hardcoded so it stays admin even if its DB row predates the is_admin column.
	isSuper := store.IsSuperAdmin(email)
	if isSuper {
		user.IsAdmin = true
	}

	respondJSON(w, http.StatusOK, struct {
		*store.User
		IsSuperAdmin bool               `json:"is_super_admin"`
		TokenUsage   tokenUsageResponse `json:"token_usage"`
	}{
		User:         user,
		IsSuperAdmin: isSuper,
		TokenUsage:   tokenUsage,
	})
}

// Why: Automatically prepopulates user aliases from Slack if none exist.
// Idempotency: Skip DB updates if SlackID or Aliases are already identical.
func (a *API) autoPopulateSlackAliases(ctx context.Context, user *store.User) {
	if a.Config.SlackToken == "" {
		return
	}

	sc := channels.NewSlackClient(a.Config.SlackToken) //nolint:contextcheck // SlackClient constructor; per-request ctx flows through individual API calls.
	slackUser, err := sc.LookupUserByEmail(user.Email)
	if err != nil || slackUser == nil {
		return
	}

	slackIDUnchanged := user.SlackID == slackUser.ID
	if !slackIDUnchanged {
		_ = store.UpdateUserSlackID(ctx, user.Email, slackUser.ID)
	}

	// SlackID가 같고 이미 alias가 있으면 alias 동기화 불필요
	if slackIDUnchanged && len(user.Aliases) > 0 {
		return
	}

	newAliases := buildSlackAliases(slackUser.RealName, slackUser.Profile.DisplayName)
	if len(newAliases) > 0 {
		_ = store.AddContactMapping(ctx, user.Email, user.Email, user.Name, strings.Join(newAliases, ","), "slack")
		a.refreshUserAliases(ctx, user)
	}
}

func buildSlackAliases(realName, displayName string) []string {
	aliases := []string{}
	if realName != "" {
		aliases = append(aliases, strings.TrimSpace(realName))
	}
	if displayName != "" && displayName != realName {
		aliases = append(aliases, strings.TrimSpace(displayName))
	}
	return aliases
}

func (a *API) refreshUserAliases(ctx context.Context, user *store.User) {
	user.Aliases, _ = store.GetUserAliasesByEmailFromCache(ctx, user.Email)
}

func (a *API) HandleGetUserAliases(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	aliases, _ := store.GetUserAliasesByEmailFromCache(r.Context(), email)
	respondJSON(w, http.StatusOK, aliases)
}

func (a *API) HandleAddAlias(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		Alias string `json:"alias"`
	}
	if !bindJSON(w, r, &req) {
		return
	}
	user, _ := store.GetOrCreateUser(r.Context(), email, "", "")
	existing, _ := store.GetUserAliasesByEmailFromCache(r.Context(), email)
	newAliases := strings.Join(append(existing, req.Alias), ",")
	if err := store.AddContactMapping(r.Context(), email, email, user.Name, newAliases, "user"); err != nil {
		handleAPIError(w, r, err, "[USER]", "Failed to add alias")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleDeleteAlias(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		Alias string `json:"alias"`
	}
	if !bindJSON(w, r, &req) {
		return
	}
	user, _ := store.GetOrCreateUser(r.Context(), email, "", "")
	existing, _ := store.GetUserAliasesByEmailFromCache(r.Context(), email)
	var kept []string
	for _, a := range existing {
		if strings.TrimSpace(a) != req.Alias {
			kept = append(kept, a)
		}
	}
	if err := store.AddContactMapping(r.Context(), email, email, user.Name, strings.Join(kept, ","), "user"); err != nil {
		handleAPIError(w, r, err, "[USER]", "Failed to delete alias")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleGetTenantAliases(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	mappings, err := store.GetContactsMappings(r.Context(), email)
	if err != nil {
		handleAPIError(w, r, err, "[USER]", "Failed to load tenant aliases")
		return
	}
	respondJSON(w, http.StatusOK, mappings)
}

func (a *API) HandleAddTenantAlias(w http.ResponseWriter, r *http.Request) {
	a.HandleAddMapping(w, r)
}

func (a *API) HandleDeleteTenantAlias(w http.ResponseWriter, r *http.Request) {
	a.HandleDeleteMapping(w, r)
}

func (a *API) HandleGetTokenUsage(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	tokenUsage := a.gatherTokenUsageStats(r.Context(), email)
	respondJSON(w, http.StatusOK, tokenUsage)
}

// cacheHitRate computes cached/cacheEligiblePrompt over the supplied model rows.
// Only models whose rate has CachedInputPerM > 0 contribute to the denominator so that
// Gemini rows (CachedInputPerM == 0) do not dilute DeepSeek's real hit rate.
// Returns (hitRate, totalCached, cacheEligiblePromptTokens).
func cacheHitRate(models []store.ModelTokenUsage) (rate float64, totalCached, eligiblePrompt int) {
	for _, m := range models {
		totalCached += m.Cached
		if rateFor(m.Model).CachedInputPerM > 0 {
			eligiblePrompt += m.Prompt
		}
	}
	if eligiblePrompt > 0 {
		rate = float64(totalCached) / float64(eligiblePrompt)
	}
	return rate, totalCached, eligiblePrompt
}

// Why: Includes daily and monthly AI token usage in the user info response for cost transparency.
// Token counts come from the aggregate daily/monthly queries; cost is priced per-model (aiRates)
// so a Gemini→DeepSeek mixed history is billed at each row's own rate.
func (a *API) gatherTokenUsageStats(ctx context.Context, email string) tokenUsageResponse {
	todayPrompt, todayCompletion, todayThinking, todayFiltered, _ := store.GetDailyTokenUsage(ctx, email)
	monthPrompt, monthCompletion, monthThinking, monthFiltered, _ := store.GetMonthlyTokenUsage(ctx, email)

	dailyModels, _ := store.GetDailyTokenUsageByModel(ctx, email)
	monthlyModels, _ := store.GetMonthlyTokenUsageByModel(ctx, email)

	dayCostIn, dayCostOut, dayCostThink := costByModel(dailyModels)
	monthCostIn, monthCostOut, monthCostThink := costByModel(monthlyModels)

	monthCacheHitRate, monthCached, _ := cacheHitRate(monthlyModels)

	return tokenUsageResponse{
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
		MonthlyByProvider:   costsByProvider(monthlyModels),
		Model:               providerDisplayName(a.Config.AIProvider),
	}
}

func (a *API) HandleGetMappings(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	mappings, err := store.GetContactsMappings(r.Context(), email)
	if err != nil {
		handleAPIError(w, r, err, "[USER]", "Failed to load mappings")
		return
	}
	respondJSON(w, http.StatusOK, mappings)
}

func (a *API) HandleAddMapping(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CanonicalID string `json:"canonical_id"`
		DisplayName string `json:"display_name"`
		Aliases     string `json:"aliases"`
		Source      string `json:"source"`
	}
	if !bindJSON(w, r, &req) {
		return
	}

	email := auth.GetUserEmail(r)
	finalID := determineCanonicalID(req.DisplayName, req.Aliases, req.CanonicalID)
	if finalID == "" {
		respondError(w, http.StatusBadRequest, "Canonical ID cannot be determined")
		return
	}

	if err := store.AddContactMapping(r.Context(), email, finalID, req.DisplayName, req.Aliases, req.Source); err != nil {
		handleMappingError(w, err, email, finalID)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func determineCanonicalID(displayName, aliases, canonicalID string) string {
	emailRegex := `(?i)([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})`
	re := regexp.MustCompile(emailRegex)
	for _, str := range []string{displayName, aliases, canonicalID} {
		if match := re.FindString(str); match != "" {
			return strings.ToLower(strings.ReplaceAll(match, " ", ""))
		}
	}
	return strings.ToLower(strings.ReplaceAll(displayName, " ", ""))
}

func handleMappingError(w http.ResponseWriter, err error, email, finalID string) {
	if strings.Contains(err.Error(), "UNIQUE") {
		logger.Warnf("[USER] mapping conflict: user=%s id=%s", email, finalID)
		respondError(w, http.StatusConflict, "Mapping already exists for this identity")
		return
	}
	logger.Errorf("[USER] add mapping failed: %v", err)
	respondError(w, http.StatusInternalServerError, "Internal Server Error")
}

func (a *API) HandleDeleteMapping(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		CanonicalID string `json:"canonical_id"`
	}
	if !bindJSON(w, r, &req) {
		return
	}
	if err := store.DeleteContactMapping(r.Context(), email, req.CanonicalID); err != nil {
		handleAPIError(w, r, err, "[USER]", "Failed to delete mapping")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleSearchContacts(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	query := r.URL.Query().Get("q")
	if query == "" {
		respondJSON(w, http.StatusOK, []store.ContactRecord{})
		return
	}

	results, err := store.SearchContacts(r.Context(), email, query)
	if err != nil {
		handleAPIError(w, r, err, "[USER]", "Failed to search contacts")
		return
	}
	respondJSON(w, http.StatusOK, results)
}

func (a *API) HandleLinkAccounts(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		TargetID int64 `json:"target_id"`
		MasterID int64 `json:"master_id"`
	}
	if !bindJSON(w, r, &req) {
		return
	}

	if req.TargetID == req.MasterID {
		respondError(w, http.StatusBadRequest, "Cannot link account to itself")
		return
	}

	if err := store.LinkContact(r.Context(), email, req.MasterID, req.TargetID); err != nil {
		handleAPIError(w, r, err, "[USER]", "Failed to link accounts")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleUnlinkAccount(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		ContactID int64 `json:"contact_id"`
	}
	if !bindJSON(w, r, &req) {
		return
	}

	if err := store.UnlinkContact(r.Context(), email, req.ContactID); err != nil {
		handleAPIError(w, r, err, "[USER]", "Failed to unlink account")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleGetLinks(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	links, err := store.GetLinkedContacts(r.Context(), email)
	if err != nil {
		handleAPIError(w, r, err, "[USER]", "Failed to load linked contacts")
		return
	}
	respondJSON(w, http.StatusOK, links)
}
