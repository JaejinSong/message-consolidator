package store

import (
	"context"
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

func AddContactMapping(ctx context.Context, email, canonicalID, displayName, aliases, source string) error {
	_, err := AddContact(ctx, email, canonicalID, displayName, aliases, source)
	return err
}

func AddContact(ctx context.Context, tenantEmail, canonicalID, displayName, aliases, source string) (int64, error) {
	if source == "" {
		source = "all"
	}
	var id int64
	err := db.QueryRowContext(ctx, SQL.AddContactMapping, tenantEmail, canonicalID, displayName, aliases, source).Scan(&id)
	if err == nil {
		UpdateContactsCache(tenantEmail, canonicalID, displayName, aliases, source)
	}
	return id, err
}

func UpsertContact(ctx context.Context, tenantEmail, canonicalID, displayName, aliases, source string) (int64, error) {
	if source == "" {
		source = "all"
	}
	var id int64
	err := db.QueryRowContext(ctx, SQL.UpsertContactMapping, tenantEmail, canonicalID, displayName, aliases, source).Scan(&id)
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

		_, err := UpsertContact(context.Background(), tenantEmail, canonicalID, finalDisplayName, finalAliases, source)
		return err
	}

	_, err := UpsertContact(context.Background(), tenantEmail, canonicalID, displayName, aliases, source)
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

	_, err := UpsertContact(context.Background(), email, canonical, name, newAliases, "whatsapp")
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


// GetContactsByIdentifiers fetches multiple contacts in a single pass to eliminate N+1 overhead in report generation.
func GetContactsByIdentifiers(ctx context.Context, tenantEmail string, identifiers []string) (map[string]*ContactRecord, map[string]bool, error) {
	if len(identifiers) == 0 {
		return make(map[string]*ContactRecord), make(map[string]bool), nil
	}

	res := make(map[string]*ContactRecord)
	ambiguous := make(map[string]bool)
	var remaining []string

	// Phase 1: Try Cache
	metadataMu.RLock()
	if mappings, ok := contactsCache[tenantEmail]; ok {
		for _, id := range identifiers {
			matches := findAllInMappings(mappings, id)
			if len(matches) > 1 {
				ambiguous[id] = true
				res[id] = matches[0]
			} else if len(matches) == 1 {
				res[id] = matches[0]
			} else {
				remaining = append(remaining, id)
			}
		}
	} else {
		remaining = identifiers
	}
	metadataMu.RUnlock()

	if len(remaining) == 0 {
		return res, ambiguous, nil
	}

	// Phase 2: Batch DB Lookup for missing ones
	return fetchContactsBatch(ctx, tenantEmail, remaining, res, ambiguous)
}

func findAllInMappings(mappings []ContactRecord, identifier string) []*ContactRecord {
	var res []*ContactRecord
	seen := make(map[string]bool)
	normalized := strings.ToLower(strings.TrimSpace(identifier))
	for i := range mappings {
		m := &mappings[i]
		if matchContact(m, normalized) {
			if !seen[m.CanonicalID] {
				res = append(res, m)
				seen[m.CanonicalID] = true
			}
		}
	}
	return res
}

func findInMappings(mappings []ContactRecord, identifier string) *ContactRecord {
	matches := findAllInMappings(mappings, identifier)
	if len(matches) > 0 {
		return matches[0]
	}
	return nil
}

func fetchContactsBatch(ctx context.Context, tenantEmail string, remaining []string, res map[string]*ContactRecord, ambiguous map[string]bool) (map[string]*ContactRecord, map[string]bool, error) {
	hits := make(map[string][]string)
	rows, err := db.QueryContext(ctx, "SELECT id, tenant_email, canonical_id, display_name, aliases, source, master_contact_id FROM contacts WHERE tenant_email = ?", tenantEmail)
	if err != nil {
		return res, ambiguous, nil
	}
	defer rows.Close()

	for rows.Next() {
		c := new(ContactRecord)
		if err := rows.Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Aliases, &c.Source, &c.MasterContactID); err == nil {
			for _, id := range remaining {
				if matchContact(c, id) {
					if !slices.Contains(hits[id], c.CanonicalID) {
						hits[id] = append(hits[id], c.CanonicalID)
						if len(hits[id]) > 1 {
							ambiguous[id] = true
						}
					}
					res[id] = c
				}
			}
		}
	}
	return res, ambiguous, nil
}

func matchContact(c *ContactRecord, identifier string) bool {
	normalized := strings.ToLower(strings.TrimSpace(identifier))
	if strings.ToLower(c.CanonicalID) == normalized || strings.ToLower(c.DisplayName) == normalized {
		return true
	}
	aliases := strings.Split(c.Aliases, ",")
	for _, a := range aliases {
		if strings.EqualFold(strings.TrimSpace(a), identifier) {
			return true
		}
	}
	return false
}

// GetContactByIdentifier provides a backward-compatible wrapper for single identity resolution.
func GetContactByIdentifier(ctx context.Context, tenantEmail, identifier string) (*ContactRecord, error) {
	res, ambig, err := GetContactsByIdentifiers(ctx, tenantEmail, []string{identifier})
	if err != nil {
		return nil, err
	}
	if ambig[identifier] {
		return nil, &AmbiguousIdentityError{Identifier: identifier}
	}
	return res[identifier], nil
}

func DeleteContactMapping(ctx context.Context, email, canonicalID string) error {
	_, err := db.ExecContext(ctx, SQL.DeleteContactMapping, email, canonicalID)
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

func GetContactByID(ctx context.Context, tenantEmail string, id int64) (*ContactRecord, error) {
	var c ContactRecord
	err := db.QueryRowContext(ctx, "SELECT id, tenant_email, canonical_id, display_name, aliases, source, master_contact_id FROM contacts WHERE tenant_email = ? AND id = ?", tenantEmail, int64(id)).
		Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Aliases, &c.Source, &c.MasterContactID)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func SearchContacts(ctx context.Context, tenantEmail, query string) ([]ContactRecord, error) {
	rows, err := db.QueryContext(ctx, SQL.SearchContacts, tenantEmail, query, query, query)
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

func LinkContact(ctx context.Context, tenantEmail string, masterID, targetID int64) error {
	if masterID == targetID {
		return fmt.Errorf("cannot link a contact to itself")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Safety Check: If the intended Master is already a Child, use ITS master instead to maintain 1-level flat tree.
	var masterParentID *int64
	err = tx.QueryRowContext(ctx, "SELECT master_contact_id FROM contacts WHERE id = ? AND tenant_email = ?", int64(masterID), tenantEmail).Scan(&masterParentID)
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
	if _, err := tx.ExecContext(ctx, SQL.UpdateContactLink, int64(masterID), tenantEmail, int64(targetID)); err != nil {
		return err
	}

	// 3. Flatten Tree: If the Target was already a Master for others, redirect them to the new Master.
	// This ensures a flat hierarchy (max 1 level depth).
	if _, err := tx.ExecContext(ctx, SQL.FlattenChildren, int64(masterID), tenantEmail, int64(targetID)); err != nil {
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

func UnlinkContact(ctx context.Context, tenantEmail string, targetID int64) error {
	_, err := db.ExecContext(ctx, SQL.UnlinkContact, tenantEmail, int64(targetID))
	if err == nil {
		metadataMu.Lock()
		delete(contactsCache, tenantEmail)
		metadataMu.Unlock()
	}
	return err
}

func GetLinkedContacts(ctx context.Context, tenantEmail string) ([]ContactRecord, error) {
	rows, err := db.QueryContext(ctx, SQL.GetLinkedContacts, tenantEmail)
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
