package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
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

	aliases, err := store.GetUserAliases(user.ID)
	if err == nil {
		user.Aliases = aliases
	}
	user.ArchiveDays = store.GetAutoArchiveDays()

	// If the user has no aliases, automatically fetch and prepopulate them from Slack 
	// to ensure task matching works correctly without manual configuration.
	if len(user.Aliases) == 0 {
		sc := channels.NewSlackClient(a.Config.SlackToken)
		slackUser, err := sc.LookupUserByEmail(user.Email)
		if err == nil && slackUser != nil {
			store.UpdateUserSlackID(user.Email, slackUser.ID)
			store.AddUserAlias(user.ID, slackUser.RealName)
			if slackUser.Profile.DisplayName != "" {
				store.AddUserAlias(user.ID, slackUser.Profile.DisplayName)
			}
			user.Aliases, _ = store.GetUserAliases(user.ID)
		}
	}

	// Return token usage along with user info
	todayPrompt, todayCompletion, _ := store.GetDailyTokenUsage(email)
	monthPrompt, monthCompletion, _ := store.GetMonthlyTokenUsage(email)

	calculateCost := func(p, c int) float64 {
		return (float64(p)*0.075 + float64(c)*0.30) / 1000000
	}

	tokenUsage := map[string]interface{}{
		"todayPrompt":     todayPrompt,
		"todayCompletion": todayCompletion,
		"todayTotal":      todayPrompt + todayCompletion,
		"todayCost":       calculateCost(todayPrompt, todayCompletion),
		"monthPrompt":     monthPrompt,
		"monthCompletion": monthCompletion,
		"monthTotal":      monthPrompt + monthCompletion,
		"monthCost":       calculateCost(monthPrompt, monthCompletion),
	}

	respondJSON(w, http.StatusOK, struct {
		*store.User
		TokenUsage map[string]interface{} `json:"token_usage"`
	}{
		User:       user,
		TokenUsage: tokenUsage,
	})
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
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to fetch user info")
		return
	}
	aliases, err := store.GetUserAliases(user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get user aliases")
		return
	}
	respondJSON(w, http.StatusOK, aliases)
}

func (a *API) HandleAddAlias(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		Alias string `json:"alias"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := store.AddUserAlias(user.ID, req.Alias); err != nil {
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := store.DeleteUserAlias(user.ID, req.Alias); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete alias")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleGetTenantAliases(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	aliases, err := store.GetTenantAliases(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, http.StatusOK, aliases)
}

func (a *API) HandleAddTenantAlias(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		Original string `json:"original"`
		Primary  string `json:"primary"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := store.AddTenantAlias(email, req.Original, req.Primary); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleDeleteTenantAlias(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		Original string `json:"original"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := store.DeleteTenantAlias(email, req.Original); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleGetTokenUsage(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	prompt, completion, err := store.GetDailyTokenUsage(email)
	if err != nil {
		logger.Errorf("[HANDLER] Failed to get prompt/completion for %s: %v", email, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	monthPrompt, monthCompletion, err := store.GetMonthlyTokenUsage(email)
	if err != nil {
		// Log error but continue with daily data if monthly fails
		logger.Errorf("[HANDLER] Failed to get monthly token usage: %v", err)
	}

	// Gemini 1.5 Flash pricing: Input $0.075/1M, Output $0.30/1M
	calculateCost := func(p, c int) float64 {
		return (float64(p)*0.075 + float64(c)*0.30) / 1000000
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"todayPrompt":     prompt,
		"todayCompletion": completion,
		"todayTotal":      prompt + completion,
		"todayCost":       calculateCost(prompt, completion),
		"monthPrompt":     monthPrompt,
		"monthCompletion": monthCompletion,
		"monthTotal":      monthPrompt + monthCompletion,
		"monthCost":       calculateCost(monthPrompt, monthCompletion),
	})
}

func (a *API) HandleGetMappings(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	mappings, err := store.GetContactsMappings(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, http.StatusOK, mappings)
}

func (a *API) HandleAddMapping(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		RepName string `json:"rep_name"`
		Aliases string `json:"aliases"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := store.AddContactMapping(email, req.RepName, req.Aliases); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleDeleteMapping(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	var req struct {
		RepName string `json:"rep_name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := store.DeleteContactMapping(email, req.RepName); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) HandleGetAchievements(w http.ResponseWriter, r *http.Request) {
	achievements, err := store.GetAchievements()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, http.StatusOK, achievements)
}

func (a *API) HandleGetUserAchievements(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ua, err := store.GetUserAchievements(user.ID)
	if err != nil {
		logger.Errorf("[HANDLER] Failed to get achievements for %s (ID:%d): %v", email, user.ID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, http.StatusOK, ua)
}
