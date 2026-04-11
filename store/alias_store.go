package store

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

func NormalizeIdentifier(id string) string {
	if id == "" {
		return ""
	}
	id = strings.ToLower(strings.TrimSpace(id))
	re := regexp.MustCompile(`\s*\(.*?\)\s*`)
	return strings.TrimSpace(re.ReplaceAllString(id, ""))
}

func NormalizeName(tenantEmail, name string) string {
	if name == "" {
		return ""
	}
	normalized := NormalizeIdentifier(name)
	metadataMu.RLock()
	defer metadataMu.RUnlock()

	if res, ok := resolveCurrentUserAlias(tenantEmail, normalized); ok {
		return res
	}
	if res, ok := resolveIdentityXCanonicalName(tenantEmail, normalized); ok {
		return res
	}
	return fallbackSystemUser(normalized, name)
}

func fallbackSystemUser(normalized, original string) string {
	for _, u := range userCache {
		if strings.ToLower(u.Name) == normalized || strings.ToLower(u.Email) == normalized {
			return u.Name
		}
	}
	return original
}

func resolveCurrentUserAlias(tenantEmail, nameLower string) (string, bool) {
	if nameLower != "me" && nameLower != "__current_user__" {
		return "", false
	}
	if u, ok := userCache[strings.ToLower(tenantEmail)]; ok {
		if strings.TrimSpace(u.Name) != "" {
			return u.Name, true
		}
		return u.Email, true
	}
	return tenantEmail, true
}

func resolveIdentityXCanonicalName(tenantEmail, nameLower string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	idType := "email"
	if !strings.Contains(nameLower, "@") {
		idType = "name"
	}

	id, err := ResolveAlias(ctx, idType, nameLower)
	if err != nil || id <= 0 {
		return "", false
	}

	var displayName string
	query := "SELECT display_name FROM contacts WHERE id = ? AND (tenant_email = ? OR tenant_email = 'all')"
	if err := db.QueryRow(query, int64(id), tenantEmail).Scan(&displayName); err == nil {
		return displayName, true
	}
	return "", false
}


func NormalizeWithCategory(tenantEmail, rawName string) (string, string, string) {
	if rawName == "" {
		return "", "", "External"
	}

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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
	
	metadataMu.RLock()
	defer metadataMu.RUnlock()

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

// GetUserAliases retrieves all identifiers associated with a user's primary contact (Cache-Aside).
func GetUserAliases(ctx context.Context, userID int) ([]string, error) {
	u, err := GetUserByID(userID)
	if err != nil {
		return []string{}, nil
	}

	metadataMu.RLock()
	mappings := contactsCache[u.Email]
	var contactID int64
	for _, m := range mappings {
		if m.CanonicalID == u.Email {
			contactID = m.ID
			break
		}
	}
	metadataMu.RUnlock()

	if contactID == 0 {
		return []string{}, nil
	}

	return GetAliasesForContact(ctx, contactID)
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

	uID := int64(userID)
	if _, err := db.ExecContext(ctx, SQL.CreateUserAlias, uID, trimmed); err != nil {
		return err
	}

	updateUserCacheAlias(uID, trimmed, true)
	return nil
}

func updateUserCacheAlias(userID int64, alias string, isAdd bool) {
	var uEmail, uName string
	metadataMu.Lock()
	
	// Why: Find user directly within the lock to avoid recursive deadlock with findUserInCacheByID.
	var targetUser *User
	for _, u := range userCache {
		if int64(u.ID) == userID {
			targetUser = u
			break
		}
	}

	if targetUser != nil {
		uEmail = targetUser.Email
		uName = targetUser.Name
		if isAdd {
			if !slices.Contains(targetUser.Aliases, alias) {
				targetUser.Aliases = append(targetUser.Aliases, alias)
			}
		} else {
			targetUser.Aliases = slices.DeleteFunc(targetUser.Aliases, func(a string) bool {
				return a == alias
			})
		}
	}
	metadataMu.Unlock()

	// Why: Move DB operation outside the metadata lock to prevent holding the mutex during I/O and avoid recursive lock in AddContactMapping.
	if isAdd && uEmail != "" {
		_ = AddContactMapping(context.Background(), "all", strings.ToLower(uEmail), uName, alias, "user")
	}
}


func findUserInCacheByID(id int64) *User {
	metadataMu.RLock()
	defer metadataMu.RUnlock()

	for _, u := range userCache {
		if int64(u.ID) == id {
			return u
		}
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

	uID := int64(userID)
	if _, err := db.ExecContext(ctx, SQL.DeleteUserAlias, uID, trimmed); err != nil {
		return err
	}

	updateUserCacheAlias(uID, trimmed, false)
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
