package handlers

import (
	"context"
	"message-consolidator/auth"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"net/http"
	"regexp"
	"strings"
)

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
	// Why: token usage is supplementary to the primary profile payload here (unlike the
	// dedicated /token-usage dashboard endpoint below), consistent with autoPopulateSlackAliases
	// above also degrading gracefully on failure — log and fall back to a best-effort response
	// instead of failing the whole /api/user request over a cost-dashboard lookup.
	tokenUsage, tokenUsageErr := services.GatherTokenUsageStats(r.Context(), email, a.Config.AIProvider)
	if tokenUsageErr != nil {
		logger.Errorf("[USER] token usage lookup failed for %s: %v", email, tokenUsageErr)
	}

	// Why: super admin is hardcoded so it stays admin even if its DB row predates the is_admin column.
	isSuper := store.IsSuperAdmin(email)
	if isSuper {
		user.IsAdmin = true
	}

	respondJSON(w, http.StatusOK, struct {
		*store.User
		IsSuperAdmin bool                        `json:"is_super_admin"`
		TokenUsage   services.TokenUsageResponse `json:"token_usage"`
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

// HandleGetTokenUsage is the dedicated cost dashboard endpoint, so a lookup failure here must
// not render as a silent zero: surface it as a 500 via the same handleAPIError pattern every
// other handler in this file uses.
func (a *API) HandleGetTokenUsage(w http.ResponseWriter, r *http.Request) {
	email := auth.GetUserEmail(r)
	tokenUsage, err := services.GatherTokenUsageStats(r.Context(), email, a.Config.AIProvider)
	if err != nil {
		handleAPIError(w, r, err, "[USER]", "Failed to fetch token usage")
		return
	}
	respondJSON(w, http.StatusOK, tokenUsage)
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
