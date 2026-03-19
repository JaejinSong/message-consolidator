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
		// Update cache
		found := false
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
		metadataMu.Unlock()
	}
	return err
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
