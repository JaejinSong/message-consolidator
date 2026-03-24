package store

import (
	"message-consolidator/logger"
	"slices"
	"strings"
)

type AliasMapping struct {
	RepName string `json:"rep_name"`
	Aliases string `json:"aliases"`
}

func InitContactsTable() {
	_, err := db.Exec(SQL.CreateContactsTable)
	if err != nil {
		logger.Errorf("Failed to initialize contacts table: %v", err)
	}
}

func GetContactsMappings(email string) ([]AliasMapping, error) {
	metadataMu.RLock()
	defer metadataMu.RUnlock()
	mappings, ok := contactsCache[email]
	if !ok {
		return []AliasMapping{}, nil
	}

	// Data Race 방지를 위해 복사본 반환
	result := make([]AliasMapping, len(mappings))
	copy(result, mappings)
	return result, nil
}

func AddContactMapping(email, repName, aliases string) error {
	_, err := db.Exec(SQL.UpsertContactMapping, email, repName, aliases)
	if err == nil {
		metadataMu.Lock()
		defer metadataMu.Unlock()
		// Update cache
		found := false
		if _, ok := contactsCache[email]; !ok {
			contactsCache[email] = []AliasMapping{}
		}
		idx := slices.IndexFunc(contactsCache[email], func(m AliasMapping) bool {
			return m.RepName == repName
		})
		if idx >= 0 {
			contactsCache[email][idx].Aliases = aliases
			found = true
		}
		if !found {
			contactsCache[email] = append(contactsCache[email], AliasMapping{RepName: repName, Aliases: aliases})
		}
	}
	return err
}

func SaveWhatsAppContact(email, number, name string) error {
	if number == "" || name == "" {
		return nil
	}

	// We append the number to aliases if the rep_name (name) already exists,
	// or create a new entry.
	// However, for WhatsApp, it's better to have name as rep_name and number as the primary alias.

	metadataMu.RLock()
	mappings := contactsCache[email]
	var currentAliases string
	exists := false
	for _, m := range mappings {
		if m.RepName == name {
			currentAliases = m.Aliases
			exists = true
			break
		}
	}
	metadataMu.RUnlock()

	newAliases := number
	if exists {
		// Check if number is already in aliases
		parts := strings.Split(currentAliases, ",")
		found := slices.ContainsFunc(parts, func(p string) bool {
			return strings.TrimSpace(p) == number
		})
		if found {
			return nil // Already mapped
		}
		newAliases = currentAliases + "," + number
	}

	return AddContactMapping(email, name, newAliases)
}

func GetNameByWhatsAppNumber(email, number string) string {
	metadataMu.RLock()
	mappings, ok := contactsCache[email]
	metadataMu.RUnlock()
	if !ok {
		return ""
	}

	for _, m := range mappings {
		parts := strings.Split(m.Aliases, ",")
		if slices.ContainsFunc(parts, func(p string) bool {
			return strings.TrimSpace(p) == number
		}) {
			return m.RepName
		}
	}
	return ""
}

func NormalizeContactName(email, rawName string) string {
	if rawName == "" {
		return ""
	}

	metadataMu.RLock()
	mappings, ok := contactsCache[email]
	metadataMu.RUnlock()
	if !ok {
		return rawName
	}

	normalizedRaw := strings.TrimSpace(strings.ToLower(rawName))

	// If it's a number, try exact match first
	for _, m := range mappings {
		aliases := strings.Split(m.Aliases, ",")
		if slices.ContainsFunc(aliases, func(alias string) bool {
			return strings.TrimSpace(strings.ToLower(alias)) == normalizedRaw
		}) {
			return m.RepName
		}
	}

	return rawName
}

func DeleteContactMapping(email, repName string) error {
	_, err := db.Exec(SQL.DeleteContactMapping, email, repName)
	if err == nil {
		metadataMu.Lock()
		defer metadataMu.Unlock()
		if mappings, ok := contactsCache[email]; ok {
			contactsCache[email] = slices.DeleteFunc(mappings, func(m AliasMapping) bool {
				return m.RepName == repName
			})
		}
	}
	return err
}
