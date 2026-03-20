package store

import (
	"message-consolidator/logger"
	"strings"
)

type AliasMapping struct {
	RepName string `json:"rep_name"`
	Aliases string `json:"aliases"`
}

func InitContactsTable() {
	query := `
	CREATE TABLE IF NOT EXISTS contacts (
		id SERIAL PRIMARY KEY,
		user_email VARCHAR(255) NOT NULL,
		rep_name VARCHAR(255) NOT NULL,
		aliases TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_email, rep_name)
	);`
	_, err := db.Exec(query)
	if err != nil {
		logger.Errorf("Failed to initialize contacts table: %v", err)
	}
}

func GetContactsMappings(email string) ([]AliasMapping, error) {
	metadataMu.RLock()
	mappings, ok := contactsCache[email]
	metadataMu.RUnlock()
	if !ok {
		return []AliasMapping{}, nil
	}
	return mappings, nil
}

func AddContactMapping(email, repName, aliases string) error {
	query := `
		INSERT INTO contacts (user_email, rep_name, aliases)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_email, rep_name)
		DO UPDATE SET aliases = EXCLUDED.aliases
	`
	_, err := db.Exec(query, email, repName, aliases)
	if err == nil {
		metadataMu.Lock()
		defer metadataMu.Unlock()
		// Update cache
		found := false
		if _, ok := contactsCache[email]; !ok {
			contactsCache[email] = []AliasMapping{}
		}
		for i, m := range contactsCache[email] {
			if m.RepName == repName {
				contactsCache[email][i].Aliases = aliases
				found = true
				break
			}
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

	metadataMu.Lock()
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
	metadataMu.Unlock()

	newAliases := number
	if exists {
		// Check if number is already in aliases
		parts := strings.Split(currentAliases, ",")
		found := false
		for _, p := range parts {
			if strings.TrimSpace(p) == number {
				found = true
				break
			}
		}
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
		for _, p := range parts {
			if strings.TrimSpace(p) == number {
				return m.RepName
			}
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
		for _, alias := range aliases {
			if strings.TrimSpace(strings.ToLower(alias)) == normalizedRaw {
				return m.RepName
			}
		}
	}

	return rawName
}
