package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"message-consolidator/logger"
	"slices"
	"strings"
	"time"

	"github.com/whatap/go-api/trace"
)

var ErrAmbiguousIdentity = errors.New("ambiguous identity match")

type AmbiguousIdentityError struct {
	Identifier string
	Emails     []string
}

func (e *AmbiguousIdentityError) Error() string {
	return "ambiguous identity match: " + e.Identifier
}

var (
	// GlobalContactDSU maintains the in-memory disjoint-set for fast canonical resolution.
	GlobalContactDSU = NewContactDSU()
)

type ContactRecord struct {
	ID              int64         `json:"id"`
	TenantEmail     string        `json:"tenant_email"`
	CanonicalID     string        `json:"canonical_id"`
	DisplayName     string        `json:"display_name"`
	Source          string        `json:"source"`
	MasterContactID sql.NullInt64 `json:"master_contact_id,omitempty"`
	ContactType     string        `json:"contact_type"`
}

func InitContactsTable() {
	db.Exec(SQL.CreateContactsTable)
	db.Exec(SQL.CreateContactAliasesTable)
	db.Exec(SQL.CreateIdentityMergeHistoryTable)
	db.Exec(SQL.CreateIdentityMergeCandidatesTable)
	
	// Why: Perform one-time migration of legacy aliases if the new table is empty.
	db.Exec(SQL.MigrateLegacyAliases)

	loadDSUFromDB()
}

func loadDSUFromDB() {
	rows, err := db.Query("SELECT id, master_contact_id FROM contacts WHERE master_contact_id IS NOT NULL")
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, masterID int64
		if err := rows.Scan(&id, &masterID); err == nil {
			GlobalContactDSU.Union(masterID, id)
		}
	}
	logger.Infof("[Identity-X] DSU initialized with persistent merge relations.")
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
	err := db.QueryRowContext(ctx, SQL.AddContactMapping, tenantEmail, canonicalID, displayName, source).Scan(&id)
	if err != nil {
		return 0, err
	}

	// Why: Identity-X requires every primary identifier to be registered as an alias for resolution.
	aliasType := "email"
	if !strings.Contains(canonicalID, "@") {
		aliasType = "name"
	}
	_ = RegisterAlias(ctx, id, aliasType, canonicalID, source, 5)

	// Why: Register Display Name as an alias to ensure ResolveAlias works with names too.
	if strings.TrimSpace(displayName) != "" && !strings.EqualFold(strings.TrimSpace(displayName), canonicalID) {
		_ = RegisterAlias(ctx, id, "name", displayName, source, 1)
	}

	// Why: Register secondary aliases provided in the legacy format for backwards compatibility and discovery.
	for _, a := range strings.Split(aliases, ",") {
		trimmed := strings.TrimSpace(a)
		if trimmed != "" {
			_ = RegisterAlias(ctx, id, "name", trimmed, source, 1)
		}
	}

	UpdateContactsCache(tenantEmail, canonicalID, displayName, source)
	return int64(id), nil
}

func UpsertContact(ctx context.Context, tenantEmail, canonicalID, displayName, aliases, source string) (int64, error) {
	if source == "" {
		source = "all"
	}
	var id int64
	err := db.QueryRowContext(ctx, SQL.UpsertContactMapping, tenantEmail, canonicalID, displayName, source).Scan(&id)
	if err == nil {
		// Why: Identity-X requires every primary identifier to be registered as an alias for resolution.
		aliasType := "email"
		if !strings.Contains(canonicalID, "@") {
			aliasType = "name"
		}
		_ = RegisterAlias(ctx, id, aliasType, canonicalID, source, 5)

		// Why: Register Display Name as an alias to ensure ResolveAlias works with names too.
		if strings.TrimSpace(displayName) != "" && !strings.EqualFold(strings.TrimSpace(displayName), canonicalID) {
			_ = RegisterAlias(ctx, id, "name", displayName, source, 1)
		}

		// Why: Register secondary aliases provided in the legacy format for backwards compatibility and discovery.
		for _, a := range strings.Split(aliases, ",") {
			trimmed := strings.TrimSpace(a)
			if trimmed != "" {
				_ = RegisterAlias(ctx, id, "name", trimmed, source, 1)
			}
		}

		UpdateContactsCache(tenantEmail, canonicalID, displayName, source)
	}
	return id, err
}

// UpdateContactsCache localizes cache update logic for reuse across manual and automatic upserts.
func UpdateContactsCache(email, canonicalID, displayName, source string) {
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
		contactsCache[email][idx].Source = source
	} else {
		// Note: ID will be 0 until a full reload from DB, which is acceptable for transient cache.
		contactsCache[email] = append(contactsCache[email], ContactRecord{
			TenantEmail: email,
			CanonicalID: canonicalID,
			DisplayName: displayName,
			Source:      source,
			ContactType: "none", // Default for manual cache update
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

		_, err := UpsertContact(context.Background(), tenantEmail, canonicalID, finalDisplayName, aliases, source)
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
	var currentCanonical string
	exists := false
	for _, m := range mappings {
		if m.DisplayName == name {
			currentCanonical = m.CanonicalID
			exists = true
			break
		}
	}
	metadataMu.RUnlock()

	canonical := strings.ToLower(name) // Fallback if not exists
	if exists {
		canonical = currentCanonical
	}

	_, err := UpsertContact(context.Background(), email, canonical, name, number, "whatsapp")
	return err
}

func GetNameByWhatsAppNumber(email, number string) string {
	id, err := ResolveAlias(context.Background(), "whatsapp", number)
	if err != nil {
		return ""
	}
	
	metadataMu.RLock()
	mappings, ok := contactsCache[email]
	metadataMu.RUnlock()
	if !ok {
		return ""
	}

	for _, m := range mappings {
		if m.ID == id {
			return m.DisplayName
		}
	}
	return ""
}

func NormalizeContactName(email, rawName string) string {
	if rawName == "" {
		return ""
	}

	// Try Identity-X resolution first
	id, err := ResolveAlias(context.Background(), "name", rawName)
	if err == nil {
		metadataMu.RLock()
		mappings, ok := contactsCache[email]
		metadataMu.RUnlock()
		if ok {
			for _, m := range mappings {
				if m.ID == id {
					return m.DisplayName
				}
			}
		}
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

	metadataMu.RLock()
	mappings := contactsCache[tenantEmail]
	for _, id := range identifiers {
		normalized := NormalizeIdentifier(id)
		if normalized == strings.ToLower(tenantEmail) {
			res[id] = &ContactRecord{ID: 0, TenantEmail: tenantEmail, CanonicalID: tenantEmail, DisplayName: "Me", ContactType: "internal"}
			continue
		}
		matches := findAllInMappings(mappings, normalized)
		if len(matches) > 1 {
			ambiguous[id] = true
			res[id] = matches[0]
		} else if len(matches) == 1 {
			res[id] = matches[0]
		} else {
			remaining = append(remaining, id)
		}
	}
	metadataMu.RUnlock()

	if len(remaining) == 0 {
		return res, ambiguous, nil
	}

	return fetchContactsBatch(ctx, tenantEmail, remaining, res, ambiguous)
}

func findAllInMappings(mappings []ContactRecord, normalized string) []*ContactRecord {
	var res []*ContactRecord
	seen := make(map[string]bool)
	for i := range mappings {
		m := &mappings[i]
		if matchContact(m, normalized) {
			if m.ID == -1 {
				return nil
			}
			if !seen[m.CanonicalID] {
				res = append(res, m)
				seen[m.CanonicalID] = true
			}
		}
	}
	return res
}


func fetchContactsBatch(ctx context.Context, tenantEmail string, remaining []string, res map[string]*ContactRecord, ambiguous map[string]bool) (map[string]*ContactRecord, map[string]bool, error) {
	all, _ := fetchAllTenantContacts(ctx, tenantEmail)
	for _, id := range remaining {
		normalized := NormalizeIdentifier(id)
		if matches := findMatchesInBatch(all, normalized); len(matches) > 0 {
			res[id] = matches[0]
			if len(matches) > 1 { ambiguous[id] = true }
			continue
		}
		if foundID, ok := resolveWithIdentityX(ctx, normalized); ok {
			res[id] = findByID(all, foundID)
			continue
		}
		recordMissing(tenantEmail, normalized)
	}
	return res, ambiguous, nil
}

func fetchAllTenantContacts(ctx context.Context, tenantEmail string) ([]ContactRecord, error) {
	var all []ContactRecord
	rows, err := db.QueryContext(ctx, "SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type FROM contacts WHERE tenant_email = ?", tenantEmail)
	if err != nil { return nil, err }
	defer rows.Close()
	for rows.Next() {
		var c ContactRecord
		if err := rows.Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Source, &c.MasterContactID, &c.ContactType); err == nil {
			all = append(all, c)
		}
	}
	return all, nil
}

func findMatchesInBatch(all []ContactRecord, normalized string) []*ContactRecord {
	var res []*ContactRecord
	for i := range all {
		if matchContact(&all[i], normalized) {
			res = append(res, &all[i])
		}
	}
	return res
}

func resolveWithIdentityX(ctx context.Context, normalized string) (int64, bool) {
	idTypes := []string{"name", "email", "whatsapp", "slack"}
	for _, t := range idTypes {
		if ids, err := ResolveAliases(ctx, t, normalized); err == nil && len(ids) > 0 {
			return ids[0], true
		}
	}
	return 0, false
}

func findByID(all []ContactRecord, id int64) *ContactRecord {
	for i := range all {
		if all[i].ID == id { return &all[i] }
	}
	return nil
}

func recordMissing(tenantEmail, normalized string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	contactsCache[tenantEmail] = append(contactsCache[tenantEmail], ContactRecord{
		ID: -1, 
		TenantEmail: tenantEmail, 
		CanonicalID: normalized, 
		DisplayName: normalized,
	})
}

func matchContact(c *ContactRecord, normalized string) bool {
	return strings.ToLower(c.CanonicalID) == normalized || strings.ToLower(c.DisplayName) == normalized
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
	err := db.QueryRowContext(ctx, "SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type FROM contacts WHERE tenant_email = ? AND id = ?", tenantEmail, int64(id)).
		Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Source, &c.MasterContactID, &c.ContactType)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func SearchContacts(ctx context.Context, tenantEmail, query string) ([]ContactRecord, error) {
	rows, err := db.QueryContext(ctx, SQL.SearchContacts, tenantEmail, query, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ContactRecord
	for rows.Next() {
		var c ContactRecord
		if err := rows.Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Source, &c.MasterContactID, &c.ContactType); err != nil {
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

	// 1. Safety Check & Type Fetch: If Master is already a Child, use its parent to keep flat tree.
	var masterType string
	var masterParentID *int64
	err = tx.QueryRowContext(ctx, "SELECT master_contact_id, contact_type FROM contacts WHERE id = ? AND tenant_email = ?", int64(masterID), tenantEmail).Scan(&masterParentID, &masterType)
	if err != nil {
		return err
	}
	if masterParentID != nil {
		masterID = *masterParentID
		if masterID == targetID {
			return fmt.Errorf("circular reference detected: target is already the master of this account")
		}
		_ = tx.QueryRowContext(ctx, "SELECT contact_type FROM contacts WHERE id = ? AND tenant_email = ?", int64(masterID), tenantEmail).Scan(&masterType)
	}

	// 2. Fetch Target Type
	var targetType string
	if err := tx.QueryRowContext(ctx, "SELECT contact_type FROM contacts WHERE id = ? AND tenant_email = ?", int64(targetID), tenantEmail).Scan(&targetType); err != nil {
		return err
	}

	// 3. Link and Promote Category
	if _, err := tx.ExecContext(ctx, SQL.UpdateContactLink, int64(masterID), tenantEmail, int64(targetID)); err != nil {
		return err
	}
	
	finalType := PromoteContactType(masterType, targetType)
	if finalType != masterType {
		if _, err := tx.ExecContext(ctx, SQL.UpdateContactType, finalType, int64(masterID)); err != nil {
			return err
		}
		trace.Step(ctx, "ContactTypePromotion", fmt.Sprintf("ID:%d promoted to %s via merge", masterID, finalType), 0, int(masterID))
		invalidateCache()
	}

	// 4. Record Merge History, DSU, and Flatten
	_, _ = tx.ExecContext(ctx, SQL.InsertMergeHistory, int64(targetID), int64(masterID), "Manual Link")
	GlobalContactDSU.Union(int64(masterID), int64(targetID))
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
		if err := rows.Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Source, &c.MasterContactID, &c.ContactType); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, nil
}

// RegisterAlias adds or updates an identifier mapping for a contact.
// Why: Standardizes the 1:N mapping and ensures all identifiers are indexed for fast resolution.
func RegisterAlias(ctx context.Context, contactID int64, idType, value, source string, trust int) error {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return nil
	}

	if _, err := db.ExecContext(ctx, SQL.AddContactAlias, int64(contactID), idType, trimmed, source, int(trust)); err != nil {
		return err
	}

	// Promotion: If email alias belongs to internal domain, promote master contact to 'internal'.
	if idType == "email" && strings.HasSuffix(trimmed, "@whatap.io") {
		return UpdateContactType(ctx, contactID, "internal")
	}

	return nil
}

func ResolveAliases(ctx context.Context, idType, value string) ([]int64, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	
	start := time.Now()
	rows, err := db.QueryContext(ctx, SQL.FindContactByAlias, trimmed, idType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rawIDs []int64
	for rows.Next() {
		var rid int64
		if err := rows.Scan(&rid); err == nil {
			rawIDs = append(rawIDs, rid)
		}
	}

	if len(rawIDs) == 0 {
		return nil, sql.ErrNoRows
	}

	seen := make(map[int64]bool)
	var canonicalIDs []int64
	for _, rid := range rawIDs {
		cid := GlobalContactDSU.Find(rid)
		if !seen[cid] {
			canonicalIDs = append(canonicalIDs, cid)
			seen[cid] = true
		}
	}
	
	elapsed := time.Since(start).Milliseconds()
	trace.Step(ctx, "IdentityResolution", fmt.Sprintf("Type: %s, Latency: %dms, Results: %d", idType, elapsed, len(canonicalIDs)), int(elapsed), 0)

	return canonicalIDs, nil
}

// ResolveAlias is a convenience wrapper for ResolveAliases when only one result is expected.
func ResolveAlias(ctx context.Context, idType, value string) (int64, error) {
	ids, err := ResolveAliases(ctx, idType, value)
	if err != nil {
		return 0, err
	}
	if len(ids) > 1 {
		return 0, ErrAmbiguousIdentity
	}
	return ids[0], nil
}

// ConflictResolveDisplayName selects the best display name based on priority: Manual > Verified > Recent.
// Why: Ensures data quality when merging identities with differing meta-information.
func ConflictResolveDisplayName(manual, verified, recent string) string {
	if manual != "" {
		return manual
	}
	if verified != "" {
		return verified
	}
	return recent
}

// PromoteContactType returns the higher ranking category between two types.
// Rank: internal(4) > partner(3) > customer(2) > none(1)
func PromoteContactType(current, newcomer string) string {
	ranks := map[string]int{"internal": 4, "partner": 3, "customer": 2, "none": 1}
	if ranks[newcomer] > ranks[current] {
		return newcomer
	}
	return current
}

// UpdateContactType updates the categorization for a contact and synchronizes cache.
// Why: Standardizes the promotion logic and ensures strict type validation.
func UpdateContactType(ctx context.Context, contactID int64, cType string) error {
	validTypes := []string{"internal", "partner", "customer", "none"}
	if !slices.Contains(validTypes, cType) {
		return fmt.Errorf("invalid contact type: %s", cType)
	}

	id := int64(contactID)
	if _, err := db.ExecContext(ctx, SQL.UpdateContactType, cType, id); err != nil {
		return err
	}

	trace.Step(ctx, "ContactTypePromotion", fmt.Sprintf("ContactID:%d set to %s", id, cType), 0, int(id))
	invalidateCache()
	return nil
}

func invalidateCache() {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	for tenant := range contactsCache {
		delete(contactsCache, tenant)
	}
}
