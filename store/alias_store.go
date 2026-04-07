package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
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

	// Phase 1: Identity-X Resolution (DSU-based)
	// Why: Performs transitive lookup (A=B, B=C => A=C) via the Disjoint-Set Union.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Try as email first, then name
	idType := "email"
	if !strings.Contains(nameLower, "@") {
		idType = "name"
	}

	if canonicalID, err := ResolveAlias(ctx, idType, nameLower); err == nil && canonicalID > 0 {
		// Fetch the display name of the canonical master
		var displayName string
		if err := db.QueryRow("SELECT display_name FROM contacts WHERE id = ? AND (tenant_email = ? OR tenant_email = 'all')", int64(canonicalID), tenantEmail).Scan(&displayName); err == nil {
			return displayName
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

	cleanName := cleanRawName(rawName)
	contact, found := resolveContactIdentity(tenantEmail, cleanName)

	resolvedName := cleanName
	foundEmail := ""
	contactType := "none"

	if found {
		resolvedName = contact.DisplayName
		foundEmail = contact.CanonicalID
		contactType = contact.ContactType
	}

	// 2. Check System Users (userCache) - Fallback or Refinement
	uName, uEmail, uFound := resolveSystemUser(resolvedName)
	if uFound {
		resolvedName = uName
		foundEmail = uEmail
		if contactType == "none" {
			contactType = "internal"
		}
	}

	return finalizeCategory(tenantEmail, cleanName, resolvedName, foundEmail, contactType)
}

func cleanRawName(rawName string) string {
	cleanName := strings.TrimSpace(rawName)
	cleanName = strings.TrimSuffix(cleanName, " (Internal)")
	return strings.TrimSuffix(cleanName, " (External)")
}

func resolveContactIdentity(tenantEmail, name string) (ContactRecord, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	nameLower := strings.ToLower(name)
	idType := "name"
	if strings.Contains(nameLower, "@") {
		idType = "email"
	}

	if id, err := ResolveAlias(ctx, idType, nameLower); err == nil && id > 0 {
		var m ContactRecord
		query := "SELECT id, canonical_id, display_name, contact_type FROM contacts WHERE id = ? AND (tenant_email = ? OR tenant_email = 'all')"
		if err := db.QueryRow(query, int64(id), tenantEmail).Scan(&m.ID, &m.CanonicalID, &m.DisplayName, &m.ContactType); err == nil {
			return m, true
		}
	}
	return ContactRecord{}, false
}

func resolveSystemUser(name string) (string, string, bool) {
	nameLower := strings.ToLower(name)
	if u, ok := userCache[nameLower]; ok {
		return u.Name, u.Email, true
	}
	for _, u := range userCache {
		if strings.EqualFold(u.Name, name) || strings.EqualFold(u.Email, name) {
			return u.Name, u.Email, true
		}
	}
	return "", "", false
}

func finalizeCategory(tenantEmail, cleanName, resolvedName, foundEmail, contactType string) (string, string, string) {
	finalID := strings.ToLower(cleanName)
	if foundEmail != "" {
		finalID = strings.ToLower(foundEmail)
	}

	category := mapContactType(contactType, finalID, tenantEmail)

	displayResult := resolvedName
	if category != "Internal" {
		displayResult = NormalizeContactName(tenantEmail, resolvedName)
	}

	return finalID, displayResult, category
}

func mapContactType(contactType, finalID, tenantEmail string) string {
	switch contactType {
	case "internal":
		return "Internal"
	case "partner":
		return "Partner"
	case "customer":
		return "Customer"
	}
	if finalID == strings.ToLower(tenantEmail) {
		return "Internal"
	}
	return "External"
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
				var aliases []string
				rows, err := db.Query("SELECT value FROM contact_aliases WHERE contact_id = ?", m.ID)
				if err == nil {
					defer rows.Close()
					for rows.Next() {
						var a string
						if err := rows.Scan(&a); err == nil {
							aliases = append(aliases, a)
						}
					}
				}
				return aliases, nil
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

// AddUserAlias creates a new alias for a user in the user_aliases table and updates the cache.
func AddUserAlias(ctx context.Context, userID int, alias string) error {
	trimmed := strings.TrimSpace(alias)
	if trimmed == "" {
		return nil
	}

	// 1. Update Database (Slow Path table)
	_, err := db.ExecContext(ctx, SQL.CreateUserAlias, userID, trimmed)
	if err != nil {
		return err
	}

	// 2. Update Cache (Write-through)
	var email string
	var uName string
	var uEmail string

	metadataMu.Lock()
	for e, u := range userCache {
		if u.ID == userID {
			email = e
			uName = u.Name
			uEmail = u.Email
			if !slices.Contains(u.Aliases, trimmed) {
				u.Aliases = append(u.Aliases, trimmed)
			}
			break
		}
	}
	metadataMu.Unlock()

	// 3. Keep Contacts mapping for global resolution consistency
	if email != "" {
		_ = AddContactMapping(ctx, "all", strings.ToLower(uEmail), uName, trimmed, "user")
	}

	return nil
}

// AddTenantAlias is a legacy compatibility helper for tenant-specific aliases.
func AddTenantAlias(ctx context.Context, tenantEmail, original, primary string) error {
	parts := strings.Split(original, ",")
	bestID := strings.TrimSpace(parts[0])
	return AddContactMapping(ctx, tenantEmail, strings.ToLower(bestID), primary, original, "legacy")
}

// DeleteUserAlias removes an alias for a user from the user_aliases table and updates the cache.
func DeleteUserAlias(ctx context.Context, userID int, alias string) error {
	trimmed := strings.TrimSpace(alias)

	// 1. Update Database
	_, err := db.ExecContext(ctx, SQL.DeleteUserAlias, userID, trimmed)
	if err != nil {
		return err
	}

	// 2. Update Cache
	metadataMu.Lock()
	defer metadataMu.Unlock()

	for _, u := range userCache {
		if u.ID == userID {
			u.Aliases = slices.DeleteFunc(u.Aliases, func(a string) bool {
				return a == trimmed
			})
			break
		}
	}

	return nil
}

// DeleteTenantAlias is a legacy compatibility helper.
func DeleteTenantAlias(ctx context.Context, tenantEmail, original string) error {
	parts := strings.Split(original, ",")
	bestID := strings.TrimSpace(parts[0])
	return DeleteContactMapping(ctx, tenantEmail, strings.ToLower(bestID))
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
