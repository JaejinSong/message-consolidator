package store

import (
	"context"
	"fmt"
	"strings"
)

// GetAliasesByContactIDs fetches aliases for multiple contacts in a single query (Batch Fetching).
// Why: Standardizes Identity-X resolution in bulk to eliminate N+1 DB roundtrips.
func GetAliasesByContactIDs(ctx context.Context, contactIDs []int64) (map[int64][]string, error) {
	if len(contactIDs) == 0 {
		return make(map[int64][]string), nil
	}

	result := make(map[int64][]string)
	var missing []int64

	// Phase 1: Try RLock Cache
	metadataMu.RLock()
	for _, id := range contactIDs {
		if cached, ok := aliasCache[id]; ok {
			result[id] = cached
		} else {
			missing = append(missing, id)
		}
	}
	metadataMu.RUnlock()

	if len(missing) == 0 {
		return result, nil
	}

	// Phase 2: Single-Flight DB Batch Fetch
	return fetchAliasesBatch(ctx, missing, result)
}

func fetchAliasesBatch(ctx context.Context, missing []int64, result map[int64][]string) (map[int64][]string, error) {
	placeholders := make([]string, len(missing))
	args := make([]interface{}, len(missing))
	for i, id := range missing {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("SELECT contact_id, identifier_value FROM contact_aliases WHERE contact_id IN (%s)", strings.Join(placeholders, ","))
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	metadataMu.Lock()
	defer metadataMu.Unlock()

	batchResults := make(map[int64][]string)
	for rows.Next() {
		var cid int64
		var val string
		if err := rows.Scan(&cid, &val); err == nil {
			batchResults[cid] = append(batchResults[cid], val)
		}
	}

	// Update cache and merge with result
	for _, id := range missing {
		aliases := batchResults[id]
		if aliases == nil {
			aliases = []string{} // Cached empty to prevent re-query
		}
		aliasCache[id] = aliases
		result[id] = aliases
	}

	return result, nil
}

// BulkResolveAliases resolves multiple names to their display names in one pass.
// Why: Enables Data Loader pattern in services to eliminate N+1 overhead during message formatting.
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
		if c, ok := res[n]; ok && c != nil {
			m[n] = c.DisplayName
		} else {
			m[n] = n
		}
	}
	return m
}
