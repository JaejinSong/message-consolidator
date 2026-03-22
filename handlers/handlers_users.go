package handlers

import (
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
)

func HandleUserInfo(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	logger.Infof("[USER] Fetching info for email: %s", email)
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		logger.Errorf("[USER] Error fetching user %s: %v", email, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logger.Debugf("[USER] Found user: ID=%d, Streak=%d, XP=%d", user.ID, user.Streak, user.XP)

	aliases, err := store.GetUserAliases(user.ID)
	if err == nil {
		user.Aliases = aliases
	}

	if len(user.Aliases) == 0 {
		sc := channels.NewSlackClient(cfg.SlackToken)
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

	respondJSON(w, user)
}

func HandleBuyStreakFreeze(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if user.Points < 50 {
		http.Error(w, "Not enough points (requires 50)", http.StatusBadRequest)
		return
	}

	_ = store.UpdateUserGamification(email, user.Points-50, user.Streak, user.Level, user.XP, user.DailyGoal, user.LastCompletedAt, user.StreakFreezes+1)

	respondJSON(w, map[string]interface{}{"success": true, "points": user.Points - 50, "streak_freezes": user.StreakFreezes + 1})
}

func HandleGetUserAliases(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	aliases, err := store.GetUserAliases(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, aliases)
}

func HandleAddAlias(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func HandleDeleteAlias(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func HandleGetTenantAliases(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	aliases, err := store.GetTenantAliases(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, aliases)
}

func HandleAddTenantAlias(w http.ResponseWriter, r *http.Request) {
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

func HandleDeleteTenantAlias(w http.ResponseWriter, r *http.Request) {
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

func HandleGetTokenUsage(w http.ResponseWriter, r *http.Request) {
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

	respondJSON(w, map[string]int{
		"todayPrompt":     prompt,
		"todayCompletion": completion,
		"todayTotal":      prompt + completion,
		"monthPrompt":     monthPrompt,
		"monthCompletion": monthCompletion,
		"monthTotal":      monthPrompt + monthCompletion,
	})
}

func HandleGetMappings(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	mappings, err := store.GetContactsMappings(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, mappings)
}

func HandleAddMapping(w http.ResponseWriter, r *http.Request) {
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

func HandleDeleteMapping(w http.ResponseWriter, r *http.Request) {
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

func HandleGetAchievements(w http.ResponseWriter, r *http.Request) {
	achievements, err := store.GetAchievements()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, achievements)
}

func HandleGetUserAchievements(w http.ResponseWriter, r *http.Request) {
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
	respondJSON(w, ua)
}
