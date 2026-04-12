package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"message-consolidator/db"
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

func InitContactsTable(q Querier) {
	if q == nil {
		q = GetDB()
	}
	_, _ = q.Exec(SQL.CreateContactsTable)
	_, _ = q.Exec(SQL.CreateContactAliasesTable)
	_, _ = q.Exec(SQL.CreateIdentityMergeHistoryTable)
	_, _ = q.Exec(SQL.CreateIdentityMergeCandidatesTable)
	
	// Why: Perform one-time migration of legacy aliases if the new table is empty.
	_, _ = q.Exec(SQL.MigrateLegacyAliases)

	// Why: DSU is handled in-memory but persisted merge relations must be loaded.
	loadDSUFromDB()
}

func loadDSUFromDB() {
	conn := GetDB()
	rows, err := conn.Query("SELECT id, master_contact_id FROM contacts WHERE master_contact_id IS NOT NULL")
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
		source = SourceAll
	}
	newID, err := db.New(GetDB()).UpsertContactMappingSimple(ctx, db.UpsertContactMappingSimpleParams{
		TenantEmail:  tenantEmail,
		CanonicalID:  canonicalID,
		DisplayName:  displayName,
		Source:       sql.NullString{String: source, Valid: true},
	})
	if err != nil {
		return 0, err
	}
	id := int64(newID)

	// Why: Identity-X requires every primary identifier to be registered as an alias for resolution.
	aliasType := "email"
	if !strings.Contains(canonicalID, "@") {
		aliasType = "name"
	}
	_ = RegisterAlias(ctx, id, aliasType, canonicalID, source, 5)

	// Why: Register Display Name as an alias to ensure ResolveAlias works with names too.
	if strings.TrimSpace(displayName) != "" && !strings.EqualFold(strings.TrimSpace(displayName), canonicalID) {
		_ = RegisterAlias(ctx, id, ContactTypeName, displayName, source, 1)
	}

	// Why: Register secondary aliases provided in the legacy format for backwards compatibility and discovery.
	for _, a := range strings.Split(aliases, ",") {
		if trimmed := NormalizeIdentifier(a); trimmed != "" {
			_ = RegisterAlias(ctx, id, ContactTypeName, trimmed, source, 1)
		}
	}

	UpdateContactsCache(int64(id), tenantEmail, canonicalID, displayName, source)
	return int64(id), nil
}

func UpsertContact(ctx context.Context, tenantEmail, canonicalID, displayName, aliases, source string) (int64, error) {
	if source == "" {
		source = SourceAll
	}
	newID, err := db.New(GetDB()).UpsertContactMapping(ctx, db.UpsertContactMappingParams{
		TenantEmail:  tenantEmail,
		CanonicalID:  canonicalID,
		DisplayName:  displayName,
		Source:       sql.NullString{String: source, Valid: true},
	})
	id := int64(newID)
	if err == nil {
		// Why: Identity-X requires every primary identifier to be registered as an alias for resolution.
		aliasType := ContactTypeEmail
		if !strings.Contains(canonicalID, "@") {
			aliasType = ContactTypeName
		}
		_ = RegisterAlias(ctx, id, aliasType, canonicalID, source, 5)

		// Why: Register Display Name as an alias to ensure ResolveAlias works with names too.
		if strings.TrimSpace(displayName) != "" && !strings.EqualFold(strings.TrimSpace(displayName), canonicalID) {
			_ = RegisterAlias(ctx, id, ContactTypeName, displayName, source, 1)
		}

		// Why: Register secondary aliases provided in the legacy format for backwards compatibility and discovery.
		for _, a := range strings.Split(aliases, ",") {
			if trimmed := NormalizeIdentifier(a); trimmed != "" {
				_ = RegisterAlias(ctx, id, ContactTypeName, trimmed, source, 1)
			}
		}

		UpdateContactsCache(id, tenantEmail, canonicalID, displayName, source)
	}
	return id, err
}

// UpdateContactsCache localizes cache update logic for reuse across manual and automatic upserts.
func UpdateContactsCache(id int64, email, canonicalID, displayName, source string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	
	if _, ok := contactsCache[email]; !ok {
		contactsCache[email] = []ContactRecord{}
	}
	idx := slices.IndexFunc(contactsCache[email], func(m ContactRecord) bool {
		return m.CanonicalID == canonicalID
	})
	if idx >= 0 {
		contactsCache[email][idx].ID = id
		contactsCache[email][idx].DisplayName = displayName
		contactsCache[email][idx].Source = source
	} else {
		contactsCache[email] = append(contactsCache[email], ContactRecord{
			ID:          id,
			TenantEmail: email,
			CanonicalID: canonicalID,
			DisplayName: displayName,
			Source:      source,
			ContactType: CategoryNone, // Default for manual cache update
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

	_, err := UpsertContact(context.Background(), email, canonical, name, number, ContactTypeWhatsApp)
	return err
}

func GetNameByWhatsAppNumber(email, number string) string {
	id, err := ResolveAlias(context.Background(), ContactTypeWhatsApp, number)
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

	metadataMu.RLock()
	mappings, ok := contactsCache[email]
	metadataMu.RUnlock()

	// Why: Lazily populate the cache if it was invalidated or not yet loaded for this tenant.
	if !ok {
		all, err := fetchAllTenantContacts(context.Background(), email)
		if err == nil {
			metadataMu.Lock()
			contactsCache[email] = all
			mappings = all
			metadataMu.Unlock()
		}
	}

	// Helper for matching
	match := func(records []ContactRecord, raw string) string {
		// 1. Precise Identity-X resolution
		id, err := ResolveAlias(context.Background(), ContactTypeName, raw)
		if err == nil {
			for _, m := range records {
				if m.ID == id {
					return m.DisplayName
				}
			}
		}
		// 2. Fuzzy/Identifier matching
		normalized := strings.TrimSpace(strings.ToLower(raw))
		for _, m := range records {
			if strings.ToLower(m.DisplayName) == normalized || strings.ToLower(m.CanonicalID) == normalized {
				return m.DisplayName
			}
		}
		return ""
	}

	if name := match(mappings, rawName); name != "" {
		return name
	}

	// Why: If not found in cache, it might be due to a recent partial cache invalidation (e.g. during promotion loop).
	// We force a reload from DB once more before giving up.
	all, err := fetchAllTenantContacts(context.Background(), email)
	if err == nil {
		metadataMu.Lock()
		contactsCache[email] = all
		metadataMu.Unlock()
		if name := match(all, rawName); name != "" {
			return name
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
		if id == tenantEmail {
			res[id] = &ContactRecord{ID: 0, TenantEmail: tenantEmail, CanonicalID: tenantEmail, DisplayName: "Me", ContactType: CategoryInternal}
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
	
	// Phase 1: Bulk resolve unknown identifiers using Identity-X
	resolvedIDs, ambigMap := BulkResolveIdentityX(ctx, remaining)
	
	// Phase 2: Map resolved IDs back to ContactRecords and handle missing/ambiguous
	for _, id := range remaining {
		normalized := NormalizeIdentifier(id)
		
		// Case A: Identity-X identified this as ambiguous
		if ambigMap[normalized] {
			ambiguous[id] = true
			res[id] = &ContactRecord{ID: -1} // Sentinel for ambiguous
			continue
		}
		
		// Case B: Identity-X found a single mapping
		if foundID, ok := resolvedIDs[normalized]; ok {
			if contact := findByID(all, foundID); contact != nil {
				res[id] = contact
				continue
			}
		}
		
		// Case C: Truly missing - negative cache it
		recordMissing(tenantEmail, id)
	}
	
	return res, ambiguous, nil
}

// BulkResolveIdentityX performs a batch lookup of identifiers in contact_aliases with SQLite-safe chunking.
// Why: Eliminates N+1 performance bottleneck by consolidating multiple identity lookups into a single IN query.
func BulkResolveIdentityX(ctx context.Context, identifiers []string) (map[string]int64, map[string]bool) {
	uniqueNormalized := make(map[string]bool)
	var normalizedList []string
	for _, id := range identifiers {
		if norm := NormalizeIdentifier(id); norm != "" && !uniqueNormalized[norm] {
			uniqueNormalized[norm] = true
			normalizedList = append(normalizedList, norm)
		}
	}

	res := make(map[string]int64)
	ambiguous := make(map[string]bool)
	if len(normalizedList) == 0 {
		return res, ambiguous
	}

	// SQLite has a parameter limit (usually 999). We use 500 for safety.
	const chunkSize = 500
	for i := 0; i < len(normalizedList); i += chunkSize {
		end := i + chunkSize
		if end > len(normalizedList) {
			end = len(normalizedList)
		}
		processResolutionChunk(ctx, normalizedList[i:end], res, ambiguous)
	}
	return res, ambiguous
}

func processResolutionChunk(ctx context.Context, chunk []string, res map[string]int64, ambiguous map[string]bool) {
	placeholders := make([]string, len(chunk))
	args := make([]interface{}, len(chunk))
	for i, val := range chunk {
		placeholders[i] = "?"
		args[i] = val
	}

	query := fmt.Sprintf("SELECT identifier_value, contact_id FROM contact_aliases WHERE identifier_value IN (%s)", strings.Join(placeholders, ","))
	conn := GetDB()
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return
	}
	defer rows.Close()

	idToCanonical := make(map[string][]int64)
	for rows.Next() {
		var val string
		var cid int64
		if err := rows.Scan(&val, &cid); err == nil {
			canonID := GlobalContactDSU.Find(cid)
			if !slices.Contains(idToCanonical[val], canonID) {
				idToCanonical[val] = append(idToCanonical[val], canonID)
			}
		}
	}

	for val, cids := range idToCanonical {
		if len(cids) > 1 {
			ambiguous[val] = true
		} else if len(cids) == 1 {
			res[val] = cids[0]
		}
	}
}


func fetchAllTenantContacts(ctx context.Context, tenantEmail string) ([]ContactRecord, error) {
	var all []ContactRecord
	conn := GetDB()
	rows, err := conn.QueryContext(ctx, "SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type FROM contacts WHERE tenant_email = ?", tenantEmail)
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
	err := db.New(GetDB()).DeleteContactMapping(ctx, db.DeleteContactMappingParams{
		TenantEmail: email,
		CanonicalID: canonicalID,
	})
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
	conn := GetDB()
	err := conn.QueryRowContext(ctx, "SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type FROM contacts WHERE tenant_email = ? AND id = ?", tenantEmail, int64(id)).
		Scan(&c.ID, &c.TenantEmail, &c.CanonicalID, &c.DisplayName, &c.Source, &c.MasterContactID, &c.ContactType)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func SearchContacts(ctx context.Context, tenantEmail, query string) ([]ContactRecord, error) {
	rows, err := db.New(GetDB()).SearchContacts(ctx, db.SearchContactsParams{
		TenantEmail: tenantEmail,
		Column2:     sql.NullString{String: query, Valid: true},
		Column3:     sql.NullString{String: query, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	var results []ContactRecord
	for _, r := range rows {
		results = append(results, ContactRecord{
			ID:              int64(r.ID),
			TenantEmail:     r.TenantEmail,
			CanonicalID:     r.CanonicalID,
			DisplayName:     r.DisplayName,
			Source:          r.Source.String,
			MasterContactID: r.MasterContactID,
			ContactType:     r.ContactType.String,
		})
	}
	return results, nil
}

func LinkContact(ctx context.Context, tenantEmail string, masterID, targetID int64) error {
	if masterID == targetID {
		return fmt.Errorf("cannot link a contact to itself")
	}

	conn := GetDB()
	tx, err := conn.BeginTx(ctx, nil)
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
	q := db.New(tx)
	if err := q.UpdateContactLink(ctx, db.UpdateContactLinkParams{
		MasterContactID: sql.NullInt64{Int64: int64(masterID), Valid: true},
		TenantEmail:     tenantEmail,
		ID:              int64(targetID),
	}); err != nil {
		return err
	}
	
	finalType := PromoteContactType(masterType, targetType)
	if finalType != masterType {
		if err := q.UpdateContactType(ctx, db.UpdateContactTypeParams{
			ContactType: sql.NullString{String: finalType, Valid: true},
			ID:          int64(masterID),
		}); err != nil {
			return err
		}
		trace.Step(ctx, "ContactTypePromotion", fmt.Sprintf("ID:%d promoted to %s via merge", masterID, finalType), 0, int(masterID))
		invalidateCache()
	}

	// 4. Record Merge History, DSU, and Flatten
	_ = q.InsertMergeHistory(ctx, db.InsertMergeHistoryParams{
		SourceContactID: int64(targetID),
		TargetContactID: int64(masterID),
		Reason:          "Manual Link",
	})
	GlobalContactDSU.Union(int64(masterID), int64(targetID))
	if err := q.FlattenChildren(ctx, db.FlattenChildrenParams{
		MasterContactID:   sql.NullInt64{Int64: int64(masterID), Valid: true},
		TenantEmail:       tenantEmail,
		MasterContactID_2: sql.NullInt64{Int64: int64(targetID), Valid: true},
	}); err != nil {
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
	err := db.New(GetDB()).UnlinkContact(ctx, db.UnlinkContactParams{
		TenantEmail: tenantEmail,
		ID:          int64(targetID),
	})
	if err == nil {
		metadataMu.Lock()
		delete(contactsCache, tenantEmail)
		metadataMu.Unlock()
	}
	return err
}

func GetLinkedContacts(ctx context.Context, tenantEmail string) ([]ContactRecord, error) {
	rows, err := db.New(GetDB()).GetLinkedContacts(ctx, tenantEmail)
	if err != nil {
		return nil, err
	}
	var results []ContactRecord
	for _, r := range rows {
		results = append(results, ContactRecord{
			ID:              int64(r.ID),
			TenantEmail:     r.TenantEmail,
			CanonicalID:     r.CanonicalID,
			DisplayName:     r.DisplayName,
			Source:          r.Source.String,
			MasterContactID: r.MasterContactID,
			ContactType:     r.ContactType.String,
		})
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

	if err := db.New(GetDB()).AddContactAlias(ctx, db.AddContactAliasParams{
		ContactID:       int64(contactID),
		IdentifierType:  idType,
		IdentifierValue: trimmed,
		Source:          source,
		TrustLevel:      sql.NullInt64{Int64: int64(trust), Valid: true},
	}); err != nil {
		return err
	}

	// Promotion: If email alias belongs to internal domain, promote master contact to 'internal'.
	if idType == ContactTypeEmail && strings.HasSuffix(trimmed, "@whatap.io") {
		return UpdateContactType(ctx, contactID, CategoryInternal)
	}

	return nil
}

func ResolveAliases(ctx context.Context, idType, value string) ([]int64, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	
	start := time.Now()
	conn := GetDB()
	query := "SELECT contact_id FROM contact_aliases WHERE identifier_value = ? AND identifier_type = ?"
	rows, err := conn.QueryContext(ctx, query, trimmed, idType)
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
	ranks := map[string]int{
		CategoryInternal: 4,
		CategoryPartner:  3,
		CategoryCustomer: 2,
		CategoryNone:     1,
	}
	if ranks[newcomer] > ranks[current] {
		return newcomer
	}
	return current
}

// UpdateContactType updates the categorization for a contact and synchronizes cache.
// Why: Standardizes the promotion logic and ensures strict type validation.
func UpdateContactType(ctx context.Context, contactID int64, cType string) error {
	validTypes := []string{CategoryInternal, CategoryPartner, CategoryCustomer, CategoryNone}
	if !slices.Contains(validTypes, cType) {
		return fmt.Errorf("invalid contact type: %s", cType)
	}

	id := int64(contactID)
	if err := db.New(GetDB()).UpdateContactType(ctx, db.UpdateContactTypeParams{
		ContactType: sql.NullString{String: cType, Valid: cType != ""},
		ID:          id,
	}); err != nil {
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
