package store

import (
	"strings"
)

func NormalizeName(tenantEmail, name string) string {
	if name == "" {
		return ""
	}

	metadataMu.RLock()
	defer metadataMu.RUnlock()

	nameLower := strings.ToLower(strings.TrimSpace(name))

	// 1. Check Tenant-specific Aliases (HIGHEST PRIORITY)
	if tenantMap, ok := tenantAliasCache[tenantEmail]; ok {
		for original, primary := range tenantMap {
			if strings.ToLower(original) == nameLower {
				return primary
			}
		}
	}

	// 2. Check Primary Names of app users
	for _, u := range userCache {
		if strings.ToLower(u.Name) == nameLower {
			return u.Name
		}
	}

	// 3. Check App User Aliases
	for userID, aliases := range aliasCache {
		for _, alias := range aliases {
			if strings.ToLower(alias) == nameLower {
				for _, u := range userCache {
					if u.ID == userID {
						return u.Name
					}
				}
			}
		}
	}

	// 4. Check Contacts Mappings (LOWEST PRIORITY)
	return NormalizeContactName(tenantEmail, name)
}

func GetTenantAliases(email string) (map[string]string, error) {
	metadataMu.RLock()
	defer metadataMu.RUnlock()

	if m, ok := tenantAliasCache[email]; ok {
		return m, nil
	}
	return make(map[string]string), nil
}

func AddTenantAlias(email, original, primary string) error {
	if original == "" || primary == "" {
		return nil
	}
	_, err := db.Exec("INSERT INTO tenant_aliases (user_email, original_name, primary_name) VALUES (?, ?, ?) ON CONFLICT (user_email, original_name) DO UPDATE SET primary_name = EXCLUDED.primary_name", email, original, primary)
	if err != nil {
		return err
	}

	metadataMu.Lock()
	if _, ok := tenantAliasCache[email]; !ok {
		tenantAliasCache[email] = make(map[string]string)
	}
	tenantAliasCache[email][original] = primary
	metadataMu.Unlock()
	return nil
}

func DeleteTenantAlias(email, original string) error {
	_, err := db.Exec("DELETE FROM tenant_aliases WHERE user_email = ? AND original_name = ?", email, original)
	if err != nil {
		return err
	}

	metadataMu.Lock()
	if _, ok := tenantAliasCache[email]; ok {
		delete(tenantAliasCache[email], original)
	}
	metadataMu.Unlock()
	return nil
}

func GetUserAliases(userID int) ([]string, error) {
	metadataMu.RLock()
	aliases, ok := aliasCache[userID]
	metadataMu.RUnlock()
	if ok {
		return aliases, nil
	}

	rows, err := db.Query("SELECT alias_name FROM user_aliases WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var newAliases []string
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			continue
		}
		newAliases = append(newAliases, alias)
	}

	metadataMu.Lock()
	aliasCache[userID] = newAliases
	metadataMu.Unlock()

	return newAliases, nil
}

func AddUserAlias(userID int, alias string) error {
	if alias == "" {
		return nil
	}
	_, err := db.Exec("INSERT INTO user_aliases (user_id, alias_name) VALUES (?, ?) ON CONFLICT (user_id, alias_name) DO NOTHING", userID, alias)
	return err
}

func DeleteUserAlias(userID int, alias string) error {
	_, err := db.Exec("DELETE FROM user_aliases WHERE user_id = ? AND alias_name = ?", userID, alias)
	return err
}
