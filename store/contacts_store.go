package store

import (
	"errors"
	"message-consolidator/logger"
	"slices"
	"strings"
)

var ErrAmbiguousIdentity = errors.New("ambiguous identity match")

type AmbiguousIdentityError struct {
	Identifier string
	Emails     []string
}

func (e *AmbiguousIdentityError) Error() string {
	return "ambiguous identity match: " + e.Identifier
}

type ContactRecord struct {
	ID          int64  `json:"id"`
	TenantEmail string `json:"tenant_email"`
	CanonicalID string `json:"canonical_id"`
	DisplayName string `json:"display_name"`
	Aliases     string `json:"aliases"`
	Source      string `json:"source"`
}

func InitContactsTable() {
	_, err := db.Exec(SQL.CreateContactsTable)
	if err != nil {
		logger.Errorf("Failed to initialize contacts table: %v", err)
	}
}

func GetContactsMappings(email string) ([]ContactRecord, error) {
	metadataMu.RLock()
	defer metadataMu.RUnlock()
	mappings, ok := contactsCache[email]
	if !ok {
		return []ContactRecord{}, nil
	}

	result := make([]ContactRecord, len(mappings))
	copy(result, mappings)
	return result, nil
}

func AddContactMapping(email, canonicalID, displayName, aliases, source string) error {
	if source == "" {
		source = "all"
	}
	_, err := db.Exec(SQL.UpsertContactMapping, email, canonicalID, displayName, aliases, source)
	if err == nil {
		UpdateContactsCache(email, canonicalID, displayName, aliases, source)
	}
	return err
}

// UpdateContactsCache localizes cache update logic for reuse across manual and automatic upserts.
func UpdateContactsCache(email, canonicalID, displayName, aliases, source string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	
	if _, ok := contactsCache[email]; !ok {
		contactsCache[email] = []ContactRecord{}
	}
	idx := slices.IndexFunc(contactsCache[email], func(m ContactRecord) bool {
		return m.CanonicalID == canonicalID
	})
	if idx >= 0 {
		contactsCache[email][idx].DisplayName = displayName
		contactsCache[email][idx].Aliases = aliases
		contactsCache[email][idx].Source = source
	} else {
		contactsCache[email] = append(contactsCache[email], ContactRecord{
			CanonicalID: canonicalID,
			DisplayName: displayName,
			Aliases:     aliases,
			Source:      source,
		})
	}
}

// AutoUpsertContact provides a safe, automatic way to register new email contacts found during ingestion.
// It merges new aliases and avoids overwriting meaningful display names with raw email addresses.
func AutoUpsertContact(tenantEmail, email, name, source string) error {
	canonicalID := strings.ToLower(strings.TrimSpace(email))
	if canonicalID == "" {
		return nil
	}

	metadataMu.RLock()
	mappings := contactsCache[tenantEmail]
	var existing *ContactRecord
	for i := range mappings {
		if mappings[i].CanonicalID == canonicalID {
			existing = &mappings[i]
			break
		}
	}
	metadataMu.RUnlock()

	newName := strings.TrimSpace(name)
	isValidName := newName != "" && !strings.Contains(newName, "@") && strings.ToLower(newName) != canonicalID

	displayName := newName
	aliases := newName
	if !isValidName {
		displayName = canonicalID
		aliases = ""
	}

	if existing != nil {
		// Defensive Update: Only update display_name if the new name is valid.
		finalDisplayName := existing.DisplayName
		if isValidName {
			finalDisplayName = newName
		}

		// Merge Aliases: Add the new name to the comma-separated list if it's not already there.
		finalAliases := existing.Aliases
		if isValidName {
			parts := strings.Split(existing.Aliases, ",")
			found := false
			for _, p := range parts {
				if strings.EqualFold(strings.TrimSpace(p), newName) {
					found = true
					break
				}
			}
			if !found {
				if finalAliases == "" {
					finalAliases = newName
				} else {
					finalAliases += "," + newName
				}
			}
		}

		return AddContactMapping(tenantEmail, canonicalID, finalDisplayName, finalAliases, source)
	}

	return AddContactMapping(tenantEmail, canonicalID, displayName, aliases, source)
}


func SaveWhatsAppContact(email, number, name string) error {
	if number == "" || name == "" {
		return nil
	}

	metadataMu.RLock()
	mappings := contactsCache[email]
	var currentAliases string
	var currentCanonical string
	exists := false
	for _, m := range mappings {
		if m.DisplayName == name {
			currentAliases = m.Aliases
			currentCanonical = m.CanonicalID
			exists = true
			break
		}
	}
	metadataMu.RUnlock()

	newAliases := number
	canonical := strings.ToLower(name) // Fallback if not exists
	if exists {
		parts := strings.Split(currentAliases, ",")
		found := slices.ContainsFunc(parts, func(p string) bool {
			return strings.TrimSpace(p) == number
		})
		if found {
			return nil
		}
		newAliases = currentAliases + "," + number
		canonical = currentCanonical
	}

	return AddContactMapping(email, canonical, name, newAliases, "whatsapp")
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
			return m.DisplayName
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

	for _, m := range mappings {
		if strings.ToLower(m.DisplayName) == normalizedRaw || strings.ToLower(m.CanonicalID) == normalizedRaw {
			return m.DisplayName
		}
		aliases := strings.Split(m.Aliases, ",")
		if slices.ContainsFunc(aliases, func(alias string) bool {
			return strings.TrimSpace(strings.ToLower(alias)) == normalizedRaw
		}) {
			return m.DisplayName
		}
	}

	return rawName
}


func GetContactByIdentifier(tenantEmail, identifier string) (*ContactRecord, error) {
	if identifier == "" {
		return nil, nil
	}

	identifier = strings.TrimSpace(identifier)

	// 1. Try Cache First (Fast Path)
	metadataMu.RLock()
	mappings, ok := contactsCache[tenantEmail]
	metadataMu.RUnlock()

	if ok {
		normalizedID := strings.ToLower(identifier)
		var matches []ContactRecord
		for _, m := range mappings {
			matchFound := false
			if strings.ToLower(m.CanonicalID) == normalizedID || strings.ToLower(m.DisplayName) == normalizedID {
				matchFound = true
			} else {
				aliases := strings.Split(m.Aliases, ",")
				for _, a := range aliases {
					if strings.EqualFold(strings.TrimSpace(a), identifier) {
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
			// Check if they point to different canonical IDs
			firstID := matches[0].CanonicalID
			isAmbiguous := false
			emails := []string{firstID}
			for i := 1; i < len(matches); i++ {
				emails = append(emails, matches[i].CanonicalID)
				if matches[i].CanonicalID != firstID {
					isAmbiguous = true
				}
			}

			if isAmbiguous {
				// We have a collision! Multiple different emails share this name/alias.
				return nil, &AmbiguousIdentityError{Identifier: identifier, Emails: emails}
			}
			// All matches point to the same email, so it's safe.
			return &matches[0], nil
		}
		if len(matches) == 1 {
			return &matches[0], nil
		}
	}

	// 2. Fallback to DB (Deep Lookup)
	rows, err := db.Query(SQL.GetContactByIdentifier, tenantEmail, identifier, identifier, identifier)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []ContactRecord
	for rows.Next() {
		var c ContactRecord
		if err := rows.Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Aliases, &c.Source); err != nil {
			return nil, err
		}
		matches = append(matches, c)
	}

	if len(matches) > 1 {
		// Even in DB, check for different canonical IDs
		firstID := matches[0].CanonicalID
		isAmbiguous := false
		emails := []string{firstID}
		for i := 1; i < len(matches); i++ {
			emails = append(emails, matches[i].CanonicalID)
			if matches[i].CanonicalID != firstID {
				isAmbiguous = true
			}
		}

		if isAmbiguous {
			return nil, &AmbiguousIdentityError{Identifier: identifier, Emails: emails}
		}
		return &matches[0], nil
	}

	if len(matches) == 0 {
		return nil, nil
	}

	return &matches[0], nil
}

func DeleteContactMapping(email, canonicalID string) error {
	_, err := db.Exec(SQL.DeleteContactMapping, email, canonicalID)
	if err == nil {
		metadataMu.Lock()
		defer metadataMu.Unlock()
		if mappings, ok := contactsCache[email]; ok {
			contactsCache[email] = slices.DeleteFunc(mappings, func(m ContactRecord) bool {
				return m.CanonicalID == canonicalID
			})
		}
	}
	return err
}

