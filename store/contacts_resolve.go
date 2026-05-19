package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"message-consolidator/db"
	"strings"
	"time"

	"github.com/whatap/go-api/trace"
)

// GetContactsByIdentifiers resolves identifiers via the contact_resolution table in a single batch pass.
func GetContactsByIdentifiers(ctx context.Context, tenantEmail string, identifiers []string) (map[string]*ContactRecord, map[string]bool, error) {
	if len(identifiers) == 0 {
		return make(map[string]*ContactRecord), make(map[string]bool), nil
	}
	res := make(map[string]*ContactRecord)
	ambiguous := make(map[string]bool)

	normToOriginals, normList, preResolved := normalizeIdentifierList(tenantEmail, identifiers)
	for k, v := range preResolved {
		res[k] = v
	}
	if len(normList) == 0 {
		return res, ambiguous, nil
	}

	rows, err := db.New(GetDB()).GetResolutionsByIdentifiers(ctx, db.GetResolutionsByIdentifiersParams{TenantEmail: tenantEmail, Identifiers: normList})
	if err != nil {
		return res, ambiguous, err
	}
	normToContactID := buildNormToContactIDMap(rows)
	contactByID := fetchContactsByIDs(ctx, distinctContactIDs(rows))
	followMasterContacts(ctx, contactByID)
	ambiguousNorms := detectDisplayNameAmbiguity(ctx, tenantEmail, normList)
	mergeIdentifierResolutions(normToOriginals, normToContactID, contactByID, ambiguousNorms, res, ambiguous)
	return res, ambiguous, nil
}

func buildNormToContactIDMap(rows []db.GetResolutionsByIdentifiersRow) map[string]int64 {
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.RawIdentifier] = row.ContactID
	}
	return out
}

func distinctContactIDs(rows []db.GetResolutionsByIdentifiersRow) []int64 {
	set := make(map[int64]bool, len(rows))
	for _, row := range rows {
		set[row.ContactID] = true
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

// Why: Maps every original identifier to its resolved ContactRecord (or marks ambiguous), inverting the normalize index used during the SQL fetch.
func mergeIdentifierResolutions(normToOriginals map[string][]string, normToContactID map[string]int64, contactByID map[int64]*ContactRecord, ambiguousNorms map[string]bool, res map[string]*ContactRecord, ambiguous map[string]bool) {
	for norm, originals := range normToOriginals {
		if ambiguousNorms[norm] {
			for _, orig := range originals {
				ambiguous[orig] = true
			}
			continue
		}
		cid, ok := normToContactID[norm]
		if !ok {
			continue
		}
		for _, orig := range originals {
			res[orig] = contactByID[cid]
		}
	}
}

func normalizeIdentifierList(tenantEmail string, identifiers []string) (normToOriginals map[string][]string, normList []string, preResolved map[string]*ContactRecord) {
	normToOriginals = make(map[string][]string)
	preResolved = make(map[string]*ContactRecord)
	for _, id := range identifiers {
		if id == tenantEmail {
			preResolved[id] = &ContactRecord{ID: 0, TenantEmail: tenantEmail, CanonicalID: tenantEmail, DisplayName: "Me", ContactType: CategoryInternal}
			continue
		}
		norm := NormalizeIdentifier(id)
		if norm == "" {
			continue
		}
		if _, seen := normToOriginals[norm]; !seen {
			normList = append(normList, norm)
		}
		normToOriginals[norm] = append(normToOriginals[norm], id)
	}
	return
}

func followMasterContacts(ctx context.Context, contactByID map[int64]*ContactRecord) {
	masterIDs := make(map[int64]bool)
	for _, c := range contactByID {
		if c.MasterContactID.Valid {
			masterIDs[c.MasterContactID.Int64] = true
		}
	}
	if len(masterIDs) == 0 {
		return
	}
	masterList := make([]int64, 0, len(masterIDs))
	for mid := range masterIDs {
		masterList = append(masterList, mid)
	}
	masterByID := fetchContactsByIDs(ctx, masterList)
	for id, c := range contactByID {
		if !c.MasterContactID.Valid {
			continue
		}
		master, ok := masterByID[c.MasterContactID.Int64]
		if !ok {
			continue
		}
		if master.DisplayName == "" {
			master.DisplayName = c.DisplayName
		}
		contactByID[id] = master
	}
}

// detectDisplayNameAmbiguity returns normalized display names that are shared by multiple unmerged contacts.
// Uses COALESCE(master_contact_id, id) from DB directly to avoid stale GlobalContactDSU false positives.
func detectDisplayNameAmbiguity(ctx context.Context, tenantEmail string, normList []string) map[string]bool {
	if len(normList) == 0 {
		return nil
	}
	// Why: raw SQL retained here. sqlc v1.30 (sqlite engine) drops the slice parameter
	// when sqlc.slice() is paired with a function-wrapped LHS (LOWER(display_name) IN ...).
	// The functional index `idx_contacts_tenant_display_name(tenant_email, LOWER(display_name))`
	// requires LOWER on the column side to be hit, so we cannot work around by pre-lowercasing.
	// Per CLAUDE.md: "raw SQL은 동적 IN절 등 sqlc 정적 분석 불가 케이스에 한정".
	placeholders := strings.Repeat(",?", len(normList))[1:]
	// any 사유: QueryContext variadic args 시그니처 — 동적 IN절 placeholder별 string/email 인자.
	args := make([]any, len(normList)+1)
	args[0] = tenantEmail
	for i, n := range normList {
		args[i+1] = n
	}
	rows, err := GetDB().QueryContext(ctx,
		"SELECT COALESCE(master_contact_id, id), LOWER(display_name) FROM contacts WHERE tenant_email = ? AND LOWER(display_name) IN ("+placeholders+")",
		args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	dnToRoots := make(map[string]map[int64]bool)
	for rows.Next() {
		var root int64
		var dn string
		if err := rows.Scan(&root, &dn); err == nil {
			if dnToRoots[dn] == nil {
				dnToRoots[dn] = make(map[int64]bool)
			}
			dnToRoots[dn][root] = true
		}
	}
	result := make(map[string]bool)
	for dn, roots := range dnToRoots {
		if len(roots) > 1 {
			result[dn] = true
		}
	}
	return result
}

// BulkResolveAliases resolves multiple names to their display names in one pass.
func BulkResolveAliases(ctx context.Context, tenantEmail string, names []string) map[string]string {
	if len(names) == 0 {
		return make(map[string]string)
	}
	res, _, err := GetContactsByIdentifiers(ctx, tenantEmail, names)
	if err != nil {
		return fallbackToOriginal(names)
	}
	return buildResolutionMap(names, res)
}

func fallbackToOriginal(names []string) map[string]string {
	m := make(map[string]string)
	for _, n := range names {
		m[n] = n
	}
	return m
}

func buildResolutionMap(names []string, res map[string]*ContactRecord) map[string]string {
	m := make(map[string]string)
	for _, n := range names {
		if c, ok := res[n]; ok && c != nil && c.DisplayName != "" {
			m[n] = c.DisplayName
		} else {
			m[n] = n
		}
	}
	return m
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

func GetContactByID(ctx context.Context, tenantEmail string, id int64) (*ContactRecord, error) {
	row, err := db.New(GetDB()).GetContactByID(ctx, db.GetContactByIDParams{
		TenantEmail: tenantEmail,
		ID:          id,
	})
	if err != nil {
		return nil, err
	}
	return &ContactRecord{
		ID:              row.ID,
		TenantEmail:     row.TenantEmail,
		CanonicalID:     row.CanonicalID,
		DisplayName:     row.DisplayName,
		Source:          row.Source.String,
		MasterContactID: row.MasterContactID,
		ContactType:     row.ContactType.String,
		SecondaryIDs:    parseSecondaryIDs(row.SecondaryIds),
	}, nil
}

func ResolveAliases(ctx context.Context, idType, value string) ([]int64, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	start := time.Now()

	query, args := buildAliasQuery(idType, trimmed)
	rows, err := GetDB().QueryContext(ctx, query, args...)
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

	canonicalIDs := deduplicateByDSU(rawIDs)
	elapsed := time.Since(start).Milliseconds()
	_ = trace.Step(ctx, "IdentityResolution", fmt.Sprintf("Type: %s, Latency: %dms, Results: %d", idType, elapsed, len(canonicalIDs)), int(elapsed), 0)
	return canonicalIDs, nil
}

// any 사유: 호출자가 QueryContext에 그대로 forward — driver가 placeholder 타입 정합성 검사.
func buildAliasQuery(idType, trimmed string) (string, []any) {
	switch idType {
	case ContactTypeWhatsApp, ContactTypeTelegram:
		return "SELECT id FROM contacts WHERE LOWER(canonical_id) = ?" +
				" UNION SELECT contacts.id FROM contacts, json_each(secondary_ids) j WHERE LOWER(j.value) = ?",
			[]any{trimmed, trimmed}
	case ContactTypeEmail:
		return "SELECT id FROM contacts WHERE LOWER(canonical_id) = ?", []any{trimmed}
	default:
		return "SELECT id FROM contacts WHERE LOWER(canonical_id) = ? OR LOWER(display_name) = ?", []any{trimmed, trimmed}
	}
}

func deduplicateByDSU(rawIDs []int64) []int64 {
	seen := make(map[int64]bool)
	result := make([]int64, 0, len(rawIDs))
	for _, rid := range rawIDs {
		cid := GlobalContactDSU.Find(rid)
		if !seen[cid] {
			result = append(result, cid)
			seen[cid] = true
		}
	}
	return result
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

// ResolvedContact holds the effective contact info after following master_contact_id.
type ResolvedContact struct {
	DisplayName string
	CanonicalID string
	ContactType string
}

// BuildContactResolver loads all contacts for a tenant once and returns a map
// from raw canonical_id → effective resolved contact (merge-aware).
func BuildContactResolver(ctx context.Context, tenantEmail string) (map[string]ResolvedContact, error) {
	contacts, err := fetchAllTenantContacts(ctx, tenantEmail)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]ContactRecord, len(contacts))
	for _, c := range contacts {
		byID[c.ID] = c
	}
	resolver := make(map[string]ResolvedContact, len(contacts))
	for _, c := range contacts {
		eff := ResolvedContact{DisplayName: c.DisplayName, CanonicalID: c.CanonicalID, ContactType: c.ContactType}
		if c.MasterContactID.Valid {
			if master, ok := byID[c.MasterContactID.Int64]; ok {
				eff.DisplayName = master.DisplayName
				eff.CanonicalID = master.CanonicalID
				eff.ContactType = master.ContactType
			}
		}
		resolver[c.CanonicalID] = eff
	}
	return resolver, nil
}

// resolveContact looks up display name, canonical ID, and type for a raw identifier.
// Falls back to the raw value when not found.
func resolveContact(resolver map[string]ResolvedContact, raw string) (display, canonical, contactType string) {
	if r, ok := resolver[raw]; ok {
		return r.DisplayName, r.CanonicalID, r.ContactType
	}
	return raw, raw, "none"
}

func EnsureContactAlias(ctx context.Context, tenantEmail, rawValue string) error {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" || strings.Contains(rawValue, "@") {
		return nil
	}

	norm := NormalizeIdentifier(rawValue)
	if norm == "" {
		return nil
	}

	q := db.New(GetDB())

	rows, err := q.GetResolutionsByIdentifiers(ctx, db.GetResolutionsByIdentifiersParams{
		TenantEmail: tenantEmail,
		Identifiers: []string{norm},
	})
	if err != nil {
		return fmt.Errorf("EnsureContactAlias: %w", err)
	}
	if len(rows) > 0 {
		return nil
	}

	masters, err := q.FindMasterContactByDisplayName(ctx, db.FindMasterContactByDisplayNameParams{
		TenantEmail: tenantEmail,
		LOWER:       rawValue,
	})
	if err != nil {
		return fmt.Errorf("EnsureContactAlias: %w", err)
	}
	if len(masters) != 1 {
		return nil
	}

	master := masters[0]
	if strings.EqualFold(master.CanonicalID, rawValue) {
		return nil
	}

	aliasID, err := q.UpsertAliasContact(ctx, db.UpsertAliasContactParams{
		TenantEmail:     tenantEmail,
		CanonicalID:     rawValue,
		DisplayName:     master.DisplayName,
		MasterContactID: nullInt64(master.ID),
	})
	// Why: ON CONFLICT DO NOTHING returns sql.ErrNoRows — alias already exists, not an error.
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("EnsureContactAlias: %w", err)
	}

	rootID := GlobalContactDSU.Find(master.ID)
	GlobalContactDSU.Union(master.ID, aliasID)
	upsertResolutionForContact(ctx, tenantEmail, rootID, rawValue, rawValue, nil)
	return nil
}
