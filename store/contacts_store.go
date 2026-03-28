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

	//Why: Returns a copy of the mapping slice to prevent data races during concurrent access.
	result := make([]AliasMapping, len(mappings))
	copy(result, mappings)
	return result, nil
}

func AddContactMapping(email, repName, aliases string) error {
	_, err := db.Exec(SQL.UpsertContactMapping, email, repName, aliases)
	if err == nil {
		metadataMu.Lock()
		defer metadataMu.Unlock()
		//Why: Synchronizes the in-memory contacts cache with the updated database state.
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

	//Why: Appends the number to aliases if the representative name exists, ensuring WhatsApp numbers map correctly to user-friendly names.

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
		//Why: Verifies if the WhatsApp number is already part of the alias list to prevent duplicate mapping.
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

	//Why: Attempts an exact match against aliases if the input is a potential contact number.
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
