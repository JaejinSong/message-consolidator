package store

import (
	"database/sql"
	"fmt"
	"strings"
)

func NormalizeName(tenantEmail, name string) string {
	if name == "" {
		return ""
	}

	metadataMu.RLock()
	defer metadataMu.RUnlock()

	nameLower := strings.ToLower(strings.TrimSpace(name))

	// Phase 0: Resolve "__CURRENT_USER__" to the user's name/email from cache
	if nameLower == "me" || nameLower == "__current_user__" {
		if u, ok := userCache[strings.ToLower(tenantEmail)]; ok {
			if strings.TrimSpace(u.Name) != "" {
				return u.Name
			}
			return u.Email
		}
		return tenantEmail
	}

	// Phase 1: SSOT Contact mapping
	if mappings, ok := contactsCache[tenantEmail]; ok {
		var matches []ContactRecord
		for _, m := range mappings {
			matchFound := false
			if strings.ToLower(m.DisplayName) == nameLower || strings.ToLower(m.CanonicalID) == nameLower {
				matchFound = true
			} else {
				parts := strings.Split(m.Aliases, ",")
				for _, p := range parts {
					if strings.ToLower(strings.TrimSpace(p)) == nameLower {
						matchFound = true
						break
					}
				}
			}
			if matchFound {
				matches = append(matches, m)
			}
		}

		if len(matches) > 1 {
			// Check for different canonical IDs
			firstID := matches[0].CanonicalID
			for i := 1; i < len(matches); i++ {
				if matches[i].CanonicalID != firstID {
					// 💡 Ambiguity Safeguard: Multiple owners found. Return original to avoid mis-identification.
					return name
				}
			}
			return matches[0].DisplayName
		}
		if len(matches) == 1 {
			return matches[0].DisplayName
		}
	}

	// Phase 2: App user names (System fallback)
	for _, u := range userCache {
		if strings.ToLower(u.Name) == nameLower || strings.ToLower(u.Email) == nameLower {
			return u.Name
		}
	}

	return name
}

func NormalizeWithCategory(tenantEmail, rawName string) (string, string, string) {
	if rawName == "" {
		return "", "", "External"
	}

	metadataMu.RLock()
	defer metadataMu.RUnlock()

	// Pre-processing: Remove existing (Internal) or (External) suffixes to prevent "Name (External) (External)"
	cleanName := strings.TrimSpace(rawName)
	cleanName = strings.TrimSuffix(cleanName, " (Internal)")
	cleanName = strings.TrimSuffix(cleanName, " (External)")

	nameLower := strings.ToLower(cleanName)
	resolvedName := cleanName
	foundEmail := ""

	// 1. Check SSOT Contacts (Highest Priority)
	lookupTenants := []string{tenantEmail, "all"}
	var matches []ContactRecord
	for _, t := range lookupTenants {
		if mappings, ok := contactsCache[t]; ok {
			for _, m := range mappings {
				isMatch := (nameLower == strings.ToLower(m.DisplayName) || nameLower == strings.ToLower(m.CanonicalID))
				aliasParts := strings.Split(m.Aliases, ",")
				if !isMatch {
					for _, p := range aliasParts {
						if nameLower == strings.ToLower(strings.TrimSpace(p)) {
							isMatch = true
							break
						}
					}
				}

				if isMatch {
					matches = append(matches, m)
				}
			}
		}
	}

	if len(matches) > 0 {
		// Check for identity conflict
		firstID := matches[0].CanonicalID
		isAmbiguous := false
		for i := 1; i < len(matches); i++ {
			if matches[i].CanonicalID != firstID {
				isAmbiguous = true
				break
			}
		}

		if isAmbiguous {
			// Ambiguous match: Treat as unknown external to prevent data contamination.
			return nameLower, cleanName, "External"
		}

		// Single identity (possibly multiple records correctly pointing to the same person)
		m := matches[0]
		resolvedName = m.DisplayName
		foundEmail = m.CanonicalID

		// Refinement: Preference for @whatap.io alias if available
		aliasParts := strings.Split(m.Aliases, ",")
		for _, p := range aliasParts {
			part := strings.TrimSpace(p)
			if strings.HasSuffix(strings.ToLower(part), "@whatap.io") {
				foundEmail = part
				break
			}
		}
		goto MatchFound
	}

MatchFound:
	// 2. Check System Users (userCache) - Fallback or Refinement
	// Even if matched in contacts, we check if the resolvedName points to a known user to get a real email.
	if u, ok := userCache[strings.ToLower(resolvedName)]; ok {
		resolvedName = u.Name
		foundEmail = u.Email
	} else {
		for _, u := range userCache {
			if strings.EqualFold(u.Name, resolvedName) || strings.EqualFold(u.Email, resolvedName) {
				resolvedName = u.Name
				foundEmail = u.Email
				break
			}
		}
	}

	// 3. Final Identity and Category Determination
	finalID := nameLower
	if foundEmail != "" {
		finalID = strings.ToLower(foundEmail)
	} else if strings.Contains(nameLower, "@") {
		finalID = nameLower
	}

	category := "External"
	if strings.HasSuffix(finalID, "@whatap.io") || finalID == strings.ToLower(tenantEmail) {
		category = "Internal"
	}

	displayResult := resolvedName
	if category == "External" {
		displayResult = NormalizeContactName(tenantEmail, resolvedName)
	}

	return finalID, displayResult, category
}

func GetTenantAliases(email string) (map[string]string, error) {
	metadataMu.RLock()
	defer metadataMu.RUnlock()

	res := make(map[string]string)
	if mappings, ok := contactsCache[email]; ok {
		for _, m := range mappings {
			res[m.CanonicalID] = m.DisplayName
		}
	}
	return res, nil
}

// GetUserAliases is a legacy compatibility helper.
func GetUserAliases(userID int) ([]string, error) {
	metadataMu.RLock()
	defer metadataMu.RUnlock()

	var email string
	for e, u := range userCache {
		if u.ID == userID {
			email = e
			break
		}
	}

	if email == "" {
		return []string{}, nil
	}

	if mappings, ok := contactsCache[email]; ok {
		for _, m := range mappings {
			if m.CanonicalID == email {
				return strings.Split(m.Aliases, ","), nil
			}
		}
	}
	return []string{}, nil
}

// GetUserByID is a helper to find a user by their integer ID from the cache.
func GetUserByID(id int) (*User, error) {
	metadataMu.RLock()
	defer metadataMu.RUnlock()
	for _, u := range userCache {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, fmt.Errorf("user with ID %d not found in cache", id)
}

// AddUserAlias is a legacy compatibility helper.
func AddUserAlias(userID int, alias string) error {
	u, err := GetUserByID(userID)
	if err != nil {
		// Try DB directly if cache is not yet ready (common in tests)
		var dbUser User
		var slackID, waJID sql.NullString
		var lastCompAt, createdAt DBTime
		err := db.QueryRow(SQL.GetUserByID, userID).Scan(
			&dbUser.ID, &dbUser.Email, &dbUser.Name, &slackID, &waJID, &dbUser.Picture,
			&dbUser.Points, &dbUser.Streak, &dbUser.Level, &dbUser.XP, &dbUser.DailyGoal,
			&lastCompAt, &createdAt, &dbUser.StreakFreezes,
		)
		if err != nil {
			return AddContactMapping("all", strings.ToLower(alias), alias, alias, "legacy-user")
		}
		u = &dbUser
	}
	return AddContactMapping("all", strings.ToLower(u.Email), u.Name, alias, "legacy-user")
}

// AddTenantAlias is a legacy compatibility helper for tenant-specific aliases.
func AddTenantAlias(tenantEmail, original, primary string) error {
	parts := strings.Split(original, ",")
	bestID := strings.TrimSpace(parts[0])
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if strings.HasSuffix(strings.ToLower(trimmed), "@whatap.io") {
			bestID = trimmed
			break
		}
	}
	return AddContactMapping(tenantEmail, strings.ToLower(bestID), primary, original, "legacy")
}

// DeleteUserAlias is a legacy compatibility helper.
func DeleteUserAlias(userID int, alias string) error {
	u, err := GetUserByID(userID)
	if err != nil {
		return nil
	}
	return DeleteContactMapping("all", strings.ToLower(u.Email))
}

// DeleteTenantAlias is a legacy compatibility helper.
func DeleteTenantAlias(tenantEmail, original string) error {
	parts := strings.Split(original, ",")
	bestID := strings.TrimSpace(parts[0])
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if strings.HasSuffix(strings.ToLower(trimmed), "@whatap.io") {
			bestID = trimmed
			break
		}
	}
	return DeleteContactMapping(tenantEmail, strings.ToLower(bestID))
}

// GetUserByWAJID searches for a user in the cache by their WhatsApp JID.
// Why: Enables fast lookup of user identity from WhatsApp's raw JID during the enrichment phase of the task pipeline.
func GetUserByWAJID(jid string) (*User, error) {
	metadataMu.RLock()
	defer metadataMu.RUnlock()

	if jid == "" {
		return nil, fmt.Errorf("empty JID provided")
	}

	for _, u := range userCache {
		if u.WAJID == jid {
			return u, nil
		}
	}
	return nil, fmt.Errorf("user with WAJID %s not found in cache", jid)
}
