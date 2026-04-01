package store

import (
	"database/sql"
	"errors"
	"fmt"
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
	ID              int64         `json:"id"`
	TenantEmail     string        `json:"tenant_email"`
	CanonicalID     string        `json:"canonical_id"`
	DisplayName     string        `json:"display_name"`
	Aliases         string        `json:"aliases"`
	Source          string        `json:"source"`
	MasterContactID sql.NullInt64 `json:"master_contact_id,omitempty"`
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
	_, err := AddContact(email, canonicalID, displayName, aliases, source)
	return err
}

func AddContact(tenantEmail, canonicalID, displayName, aliases, source string) (int64, error) {
	if source == "" {
		source = "all"
	}
	var id int64
	err := db.QueryRow(SQL.AddContactMapping, tenantEmail, canonicalID, displayName, aliases, source).Scan(&id)
	if err == nil {
		UpdateContactsCache(tenantEmail, canonicalID, displayName, aliases, source)
	}
	return id, err
}

func UpsertContact(tenantEmail, canonicalID, displayName, aliases, source string) (int64, error) {
	if source == "" {
		source = "all"
	}
	var id int64
	err := db.QueryRow(SQL.UpsertContactMapping, tenantEmail, canonicalID, displayName, aliases, source).Scan(&id)
	if err == nil {
		UpdateContactsCache(tenantEmail, canonicalID, displayName, aliases, source)
	}
	return id, err
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
		// Note: ID will be 0 until a full reload from DB, which is acceptable for transient cache.
		contactsCache[email] = append(contactsCache[email], ContactRecord{
			TenantEmail: email,
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

		_, err := UpsertContact(tenantEmail, canonicalID, finalDisplayName, finalAliases, source)
		return err
	}

	_, err := UpsertContact(tenantEmail, canonicalID, displayName, aliases, source)
	return err
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

	_, err := UpsertContact(email, canonical, name, newAliases, "whatsapp")
	return err
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
		if err := rows.Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Aliases, &c.Source, &c.MasterContactID); err != nil {
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

	res := &matches[0]
	// Why: Resolve master contact if linked to provide unified identity.
	if res.MasterContactID.Valid {
		master, err := GetContactByID(tenantEmail, res.MasterContactID.Int64)
		if err == nil && master != nil {
			return master, nil
		}
	}

	return res, nil
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
func GetContactByID(tenantEmail string, id int64) (*ContactRecord, error) {
	var c ContactRecord
	err := db.QueryRow("SELECT id, tenant_email, canonical_id, display_name, aliases, source, master_contact_id FROM contacts WHERE tenant_email = ? AND id = ?", tenantEmail, id).
		Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Aliases, &c.Source, &c.MasterContactID)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func SearchContacts(tenantEmail, query string) ([]ContactRecord, error) {
	rows, err := db.Query(SQL.SearchContacts, tenantEmail, query, query, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ContactRecord
	for rows.Next() {
		var c ContactRecord
		if err := rows.Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Aliases, &c.Source, &c.MasterContactID); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, nil
}

func LinkContact(tenantEmail string, masterID, targetID int64) error {
	if masterID == targetID {
		return fmt.Errorf("cannot link a contact to itself")
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Safety Check: If the intended Master is already a Child, use ITS master instead to maintain 1-level flat tree.
	var masterParentID *int64
	err = tx.QueryRow("SELECT master_contact_id FROM contacts WHERE id = ? AND tenant_email = ?", masterID, tenantEmail).Scan(&masterParentID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if masterParentID != nil {
		masterID = *masterParentID
		if masterID == targetID {
			return fmt.Errorf("circular reference detected: target is already the master of this account")
		}
	}

	// 2. Link the target to the master
	if _, err := tx.Exec(SQL.UpdateContactLink, masterID, tenantEmail, targetID); err != nil {
		return err
	}

	// 3. Flatten Tree: If the Target was already a Master for others, redirect them to the new Master.
	// This ensures a flat hierarchy (max 1 level depth).
	if _, err := tx.Exec(SQL.FlattenChildren, masterID, tenantEmail, targetID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Simple cache invalidation
	metadataMu.Lock()
	delete(contactsCache, tenantEmail)
	metadataMu.Unlock()

	return nil
}

func UnlinkContact(tenantEmail string, targetID int64) error {
	_, err := db.Exec(SQL.UnlinkContact, tenantEmail, targetID)
	if err == nil {
		metadataMu.Lock()
		delete(contactsCache, tenantEmail)
		metadataMu.Unlock()
	}
	return err
}

func GetLinkedContacts(tenantEmail string) ([]ContactRecord, error) {
	rows, err := db.Query(SQL.GetLinkedContacts, tenantEmail)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ContactRecord
	for rows.Next() {
		var c ContactRecord
		if err := rows.Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Aliases, &c.Source, &c.MasterContactID); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, nil
}
