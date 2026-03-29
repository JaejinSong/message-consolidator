package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"regexp"
	"strings"
)

func (a *API) HandleUserInfo(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	logger.Infof("[USER] Fetching info for email: %s", email)
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch user info")
		return
	}
	logger.Debugf("[USER] Found user: ID=%d, Streak=%d, XP=%d", user.ID, user.Streak, user.XP)

	//Why: Populates aliases from the consolidated contacts cache for the current user.
	user.Aliases = []string{}
	if mappings, ok := store.GetContactsCache()[email]; ok {
		for _, m := range mappings {
			if m.CanonicalID == email {
				user.Aliases = strings.Split(m.Aliases, ",")
				break
			}
		}
	}
	user.ArchiveDays = store.GetAutoArchiveDays()

	a.autoPopulateSlackAliases(user)
	tokenUsage := gatherTokenUsageStats(email)

	respondJSON(w, http.StatusOK, struct {
		*store.User
		TokenUsage map[string]interface{} `json:"token_usage"`
	}{
		User:       user,
		TokenUsage: tokenUsage,
	})
}

// Why: Automatically prepopulates user aliases from Slack if none exist to ensure immediate task matching functionality without requiring manual user configuration.
func (a *API) autoPopulateSlackAliases(user *store.User) {
	if len(user.Aliases) > 0 || a.Config.SlackToken == "" {
		return
	}

	sc := channels.NewSlackClient(a.Config.SlackToken)
	slackUser, err := sc.LookupUserByEmail(user.Email)
	if err != nil || slackUser == nil {
		return
	}

	store.UpdateUserSlackID(user.Email, slackUser.ID)
	
	aliases := []string{}
	if slackUser.RealName != "" {
		aliases = append(aliases, slackUser.RealName)
	}
	if slackUser.Profile.DisplayName != "" && slackUser.Profile.DisplayName != slackUser.RealName {
		aliases = append(aliases, slackUser.Profile.DisplayName)
	}

	if len(aliases) > 0 {
		store.AddContactMapping(user.Email, user.Email, user.Name, strings.Join(aliases, ","), "slack")
	}

	// Refresh cache
	if mappings, ok := store.GetContactsCache()[user.Email]; ok {
		for _, m := range mappings {
			if m.CanonicalID == user.Email {
				user.Aliases = strings.Split(m.Aliases, ",")
				break
			}
		}
	}
}

func (a *API) HandleBuyStreakFreeze(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch user info")
		return
	}

	if user.Points < 50 {
		respondError(w, http.StatusBadRequest, "Not enough points (requires 50)")
		return
	}

	_ = store.UpdateUserGamification(email, user.Points-50, user.Streak, user.Level, user.XP, user.DailyGoal, user.LastCompletedAt, user.StreakFreezes+1)

	respondJSON(w, http.StatusOK, map[string]interface{}{"success": true, "points": user.Points - 50, "streak_freezes": user.StreakFreezes + 1})
}

func (a *API) HandleGetUserAliases(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var aliases []string
	if mappings, ok := store.GetContactsCache()[email]; ok {
		for _, m := range mappings {
			if m.CanonicalID == email {
				aliases = strings.Split(m.Aliases, ",")
				break
			}
		}
	}
	respondJSON(w, http.StatusOK, aliases)
}

func (a *API) HandleAddAlias(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		Alias string `json:"alias"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	
	// Map to Contact mapping for the user themselves
	user, _ := store.GetOrCreateUser(email, "", "")
	existing := ""
	if mappings, ok := store.GetContactsCache()[email]; ok {
		for _, m := range mappings {
			if m.CanonicalID == email {
				existing = m.Aliases
				break
			}
		}
	}
	
	newAliases := req.Alias
	if existing != "" {
		newAliases = existing + "," + req.Alias
	}

	if err := store.AddContactMapping(email, email, user.Name, newAliases, "user"); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to add alias")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleDeleteAlias(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		Alias string `json:"alias"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	user, _ := store.GetOrCreateUser(email, "", "")
	existing := ""
	if mappings, ok := store.GetContactsCache()[email]; ok {
		for _, m := range mappings {
			if m.CanonicalID == email {
				existing = m.Aliases
				break
			}
		}
	}

	parts := strings.Split(existing, ",")
	newParts := []string{}
	for _, p := range parts {
		if strings.TrimSpace(p) != req.Alias {
			newParts = append(newParts, p)
		}
	}

	if err := store.AddContactMapping(email, email, user.Name, strings.Join(newParts, ","), "user"); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete alias")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleGetTenantAliases(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	mappings, err := store.GetContactsMappings(email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
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
	tokenUsage := gatherTokenUsageStats(email)
	respondJSON(w, http.StatusOK, tokenUsage)
}

// Why: Includes daily and monthly AI token usage data in the user info response to provide transparency on service costs and resource consumption.
func gatherTokenUsageStats(email string) map[string]interface{} {
	todayPrompt, todayCompletion, _ := store.GetDailyTokenUsage(email)
	monthPrompt, monthCompletion, _ := store.GetMonthlyTokenUsage(email)

	calculateCost := func(p, c int) float64 {
		//Why: Calculates estimated costs based on Gemini 1.5 Flash public pricing ($0.075 per 1M input tokens, $0.30 per 1M output tokens).
		return (float64(p)*0.075 + float64(c)*0.30) / 1000000
	}

	return map[string]interface{}{
		"todayPrompt":     todayPrompt,
		"todayCompletion": todayCompletion,
		"todayTotal":      todayPrompt + todayCompletion,
		"todayCost":       calculateCost(todayPrompt, todayCompletion),
		"monthPrompt":     monthPrompt,
		"monthCompletion": monthCompletion,
		"monthTotal":      monthPrompt + monthCompletion,
		"monthCost":       calculateCost(monthPrompt, monthCompletion),
	}
}

func (a *API) HandleGetMappings(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	mappings, err := store.GetContactsMappings(email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, mappings)
}

func (a *API) HandleAddMapping(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		CanonicalID string `json:"canonical_id"`
		DisplayName string `json:"display_name"`
		Aliases     string `json:"aliases"`
		Source      string `json:"source"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Why: Enforce strict canonical_id determination logic according to user-defined Priority 1 & 2.
	// 1. Search for email in Target Name, Aliases, or the requested CanonicalID.
	emailRegex := `(?i)([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})`
	re := regexp.MustCompile(emailRegex)
	
	finalID := ""
	searchIn := []string{req.DisplayName, req.Aliases, req.CanonicalID}
	for _, str := range searchIn {
		match := re.FindString(str)
		if match != "" {
			// Rule: 소문자화 + 모든 공백 제거
			finalID = strings.ToLower(strings.ReplaceAll(match, " ", ""))
			break
		}
	}

	// 2. If no email found, use DisplayName (Target Name) as ID.
	if finalID == "" {
		// Rule: Target Name 자체를 [소문자화 + 모든 공백 제거] 하여 canonical_id로 확정
		finalID = strings.ToLower(strings.ReplaceAll(req.DisplayName, " ", ""))
	}

	// Final Safety Check: 어떠한 경우에도 대문자나 모든 형태의 공백을 포함해서는 안 됨.
	finalID = strings.ToLower(strings.ReplaceAll(finalID, " ", ""))

	if finalID == "" {
		respondError(w, http.StatusBadRequest, "Canonical ID cannot be determined (DisplayName or Email required)")
		return
	}

	// Default source to 'all' if empty
	source := req.Source
	if source == "" {
		source = "all"
	}

	if err := store.AddContactMapping(email, finalID, req.DisplayName, req.Aliases, source); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleDeleteMapping(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		CanonicalID string `json:"canonical_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := store.DeleteContactMapping(email, req.CanonicalID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleGetAchievements(w http.ResponseWriter, r *http.Request) {
	achievements, err := store.GetAchievements()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, achievements)
}

func (a *API) HandleGetUserAchievements(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ua, err := store.GetUserAchievements(user.ID)
	if err != nil {
		logger.Errorf("[HANDLER] Failed to get achievements for %s (ID:%d): %v", email, user.ID, err)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, ua)
}
