# 09. Identity Resolution & Deduplication

> Cross-ref: → [02-domain-model.md], → [04-data-layer.md] (alias/contact tables),
> → [07-ai-filter-pipeline.md], → [08-services-business-logic.md] (task_routing)

---

## 1. Problem Definition

The same physical person surfaces under fundamentally different identifiers across four channels:

| Channel | Example Identifier Form |
|---------|------------------------|
| Slack | `U04XXXXXX` (user ID) or display name `John Smith` |
| Gmail | `john.smith@example.com` |
| Telegram | numeric `UserID` (e.g. `7012345678`) |
| WhatsApp | `+66812345678` (phone JID) |

Without unification, the task pipeline treats each cross-channel identifier as a distinct person. This breaks two critical downstream functions:

**Task routing** (`services/task_routing.go`): `HandleTaskState` assigns tasks to an `assignee` string. If "John Smith" from Slack and `john.smith@example.com` from Gmail are stored as separate identities, tasks assigned to each cannot be rolled up, deduplication across channels fails, and stale-resolution logic (`GetLatestThreadAssignee`) cannot find the prior assignee in a thread.

**Report aggregation** (`services/reports_service.go`): Executive reports group activity by person. Fragmented identity produces inflated headcounts, split activity timelines, and incorrect "most active contact" rankings.

The system solves this through a layered approach:
1. **Deterministic normalization** — canonical string transformations applied on ingestion.
2. **DSU-backed in-memory graph** — O(α(N)) merge/find for real-time resolution.
3. **AI-proposed merge candidates** — Gemini analyzes ambiguous display names and proposes groupings a human can confirm.
4. **Token-sorted heuristics** — catches reversed-order names without AI cost.

---

## 2. Data Model Summary

The full schema (DDL, indexes, all columns) is documented in → [04-data-layer.md]. The tables involved in identity resolution form two layers:

- **Persistent identity graph**: `contacts` (canonical_id, master_contact_id self-ref), `contact_resolution` (O(1) hot lookup: raw identifier → contact_id), `user_aliases` / `tenant_aliases` (name normalization).
- **Proposal queue**: `identity_merge_candidates` (status: pending/accepted/rejected), `identity_merge_history` (audit trail of confirmed merges).

Key design decisions:
- `contacts.master_contact_id` is a **self-referential FK**; a slave contact points to its master. `FlattenContactChildren` ensures no grandparent chains can form — all children are re-pointed to the ultimate root on link.
- `contact_resolution` is the **hot lookup path**: every raw identifier (normalized) maps to a `contact_id` in O(1). The table is rebuilt on unlink via `RebuildContactResolution`.
- `identity_merge_candidates` serves as the **proposal queue**. Rows carry a `status` of `pending`, `accepted`, or `rejected`.
- `tenant_aliases` stores user-curated name-to-name mappings for a tenant (e.g. "Boss" → "Khun Somchai"). Separate from `user_aliases` which maps internal system users to their alternate display strings.

---

## 3. Identity Resolver (AI)

### 3.1 Responsibility

`ai/identity_resolver.go` — `IdentityResolver` struct — is the AI gateway for **fuzzy identity grouping**. It does not write to the database directly; it returns `[]MergeGroup` proposals that the handler layer persists into `identity_merge_candidates`.

```
IdentityResolver.ProposeGroups(ctx, []ContactRecord) → []MergeGroup
```

Each `MergeGroup` carries:
- `ContactIDs []int64` — the proposed merge set.
- `Confidence float64` — model-assigned probability.
- `Reason string` — human-readable explanation for UI display.

### 3.2 Why AI Is Necessary

Deterministic string matching cannot handle:
- **Transliterations**: "Phathit Chulothok" vs "พัฒิต จุฬาทก" (romanization variant).
- **Nickname ambiguity**: "Jane" could be Jane Doe (customer) or Jane Kim (internal).
- **Partial name matches**: "Smith, J." vs "John Smith" — one token is shared, but so might it be for two distinct Smiths.
- **Cross-channel display name drift**: Slack display names are user-set free text; Gmail names come from contacts; WhatsApp uses phone address books.

The Jaro-Winkler similarity (`store/similarity.go`) handles fuzzy string distance locally (threshold 0.85 per TECH.md), but it is insufficient alone — it produces false positives for short common names. Gemini provides semantic disambiguation with a stated confidence score.

### 3.3 Chunking Strategy

The `ProposeGroups` call chunks its input at `identityChunkSize = 20`:

```
if len(contacts) > 20 → proposeInChunks → sequential 20-contact windows
```

This exists because context-sensitive grouping degrades when the prompt carries too many candidates — the model may miss cross-group relationships. A tenant with 208 contacts is pre-filtered to ~49 candidates by `GetCandidateContacts` (name-substring SQL join), reducing AI chunk calls from 11 to 3.

The Gemini call uses `temperature=0.1` (near-deterministic) with a 300-second timeout and 2 retries. The response is expected as a raw JSON array; markdown code fences are stripped before `json.Unmarshal`.

### 3.4 Proposal Pipeline (Handler Layer)

The full async pipeline in `handlers/handlers_identity.go`:

```
POST /api/identity/proposals/generate
    │
    ├─ 1. AutoMergeByCanonicalID    — deterministic: same email = auto-link
    ├─ 2. GetCandidateContacts      — SQL pre-filter (name-substr join)
    ├─ 3. LoadHandledPairs          — skip already-decided pairs
    ├─ 4. insertAIProposalGroups    — Gemini ProposeGroups → InsertProposalGroup
    └─ 5. insertTokenSortedProposals — reversed-name heuristic (no AI cost)
                                        → 202 Accepted, poll /job-status
```

The job runs in a background goroutine. `proposalJobs` (in-memory map guarded by `proposalJobsMu`) tracks per-tenant job state (`running | done | error | idle`). Duplicate triggers for the same tenant are rejected with HTTP 409.

### 3.5 commit ceaf099 — GetLatestThreadAssignee

The most recent identity-aware addition is `GetLatestThreadAssignee` (`store/message_store.go`, commit ceaf099):

```go
// GetLatestThreadAssignee returns the most recent non-shared assignee in a thread (incl. done).
// Why: completion fallback path may INSERT a new task when the thread has no incomplete parent;
// surfacing the prior assignee preserves thread routing instead of dumping to "shared".
func GetLatestThreadAssignee(ctx context.Context, q Querier, email, threadID string) (string, error)
```

The underlying SQL query:
```sql
SELECT COALESCE(assignee, '') AS assignee
FROM v_messages
WHERE user_email = ? AND thread_id = ? AND is_deleted = 0
  AND IFNULL(assignee, '') != '' AND assignee != 'shared'
ORDER BY created_at DESC
LIMIT 1;
```

**Problem it solves**: In Slack, a completion event may fire on a thread whose only incomplete task has already been swept or deleted. Without this, the completion service creates a *new* task row with `assignee = 'shared'` — losing the routing context for the entire thread. `GetLatestThreadAssignee` walks back through the thread's history (including done tasks) to find the last non-shared assignee, then propagates it into the fallback-created row's `RequesterCanonical` and envelope fields (`scanner/scanner_slack.go`: `dispatchOutgoingCompletionIfMine`).

This is identity-aware because the returned assignee is the **resolved canonical name** (stored post-normalization), not a raw display string.

---

## 4. DSU (Disjoint Set Union / Union-Find)

**File**: `store/dsu.go` — `ContactDSU` struct.

### 4.1 Purpose

When a merge operation is confirmed, the system must answer: "What is the canonical root ID for this contact?" in O(1) amortized time. A flat `master_contact_id` lookup in SQL would require following a chain of `SELECT` statements for deep trees. `ContactDSU` provides this in process memory.

A single **global instance** is maintained:
```go
var GlobalContactDSU = NewContactDSU()
```

It is initialized at startup from the database via `loadDSUFromDB`, which iterates all `(id, master_contact_id)` pairs and calls `Union`:

```go
for _, row := range rows {
    if row.MasterContactID.Valid {
        GlobalContactDSU.Union(row.MasterContactID.Int64, row.ID)
    }
}
```

### 4.2 Algorithm

`ContactDSU` implements two standard optimizations:

| Optimization | Implementation | Effect |
|---|---|---|
| **Path compression** | `findInternal` flattens each traversed node to root | Subsequent finds are O(1) |
| **Union by rank** | `linkRoots` attaches the smaller-rank tree under the larger | Prevents O(N) degenerate chains |

Combined, these yield **O(α(N)) amortized** per operation, where α is the inverse Ackermann function — effectively constant for any realistic N.

### 4.3 Concurrency Safety

The DSU is protected by `sync.RWMutex`:
- `Find` acquires a **write lock** (not read) because path compression mutates `parent`. This is a deliberate trade-off: read-write locking would require promoting to a write lock mid-traversal, which is not supported without re-locking.
- `Union` acquires a write lock.
- `Reset` acquires a write lock.

### 4.4 DB Sync Points

The DSU is kept consistent with the database at two mutation points:
- **`LinkContact`** (manual merge): calls `GlobalContactDSU.Union(masterID, targetID)` inside the transaction commit path (`applyLinkUpdates`).
- **`appendSecondaryID`** (WhatsApp/Telegram channel merge): calls `GlobalContactDSU.Find(contactID)` to get the root, then upserts `contact_resolution` with the root ID.
- **`loadDSUFromDB`** (startup): full rebuild from `contacts.master_contact_id`.

The DSU is **not persisted** — it is always reconstructed from the DB on process restart. This means a crash does not leave the DSU in an inconsistent state; the next boot rebuilds from ground truth.

### 4.5 deduplicateByDSU

`ResolveAliases` in `contacts_store.go` uses the DSU to collapse multiple raw matches into their canonical roots before returning IDs:

```go
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
```

This prevents the same merged person from appearing twice in a multi-identifier lookup (e.g. WhatsApp number + email both hit contacts that have been linked).

---

## 5. Alias Normalization

### 5.1 NormalizeIdentifier

All raw identifiers flow through `store.NormalizeIdentifier` before being stored in `contact_resolution.raw_identifier`:

```
ToLower → TrimSpace → strip parenthetical suffixes → strip decorative edge chars ([-~*=_| ])
```

Decorative edge stripping (`decorativeEdgeRe`) handles edge cases from Slack display names that include status decorators like `~Jane Smith~` or `| Eng |`.

### 5.2 Self-Reference Normalization (`나`, `__CURRENT_USER__`, `me`)

The AI extraction pipeline produces assignees like `나` (Korean first-person pronoun), `me`, or `__CURRENT_USER__`. These must be resolved to the authenticated tenant's canonical name before storage.

**Token definitions** (`store/assignee_tokens.go`):
```go
const (
    AssigneeMe          = "me"
    AssigneeCurrentUser = "__current_user__"
)
```

`IsSelfAssigneeToken(s string) bool` returns `true` for either.

**Resolution path** (`store/alias_store.go` — `NormalizeName`):

```
NormalizeName(ctx, tenantEmail, rawName)
    │
    ├─ NormalizeIdentifier(name)           → lowercased, stripped
    ├─ IsSelfAssigneeToken(raw lowercase)  → preserve __current_user__ BEFORE
    │                                         NormalizeIdentifier strips underscores
    ├─ resolveCurrentUserAlias(tenantEmail, normalized)
    │       → userCache[tenantEmail].Name  (or Email fallback)
    ├─ resolveIdentityXCanonicalName(ctx, tenantEmail, normalized)
    │       → contact_resolution lookup → ContactRecord.DisplayName
    └─ fallbackSystemUser(normalized, original)
            → userCache linear scan by Name/Email
```

**Critical gotcha** (documented in the source comment):
`NormalizeIdentifier` strips leading/trailing underscores, transforming `__current_user__` into `current_user`. `IsSelfAssigneeToken` must therefore be called on the **raw lowercase input** before normalization, not after. The code explicitly checks the raw form first.

### 5.3 tenant_aliases

`tenant_aliases` provides tenant-level name remapping independent of the contact graph:

```sql
CREATE TABLE IF NOT EXISTS tenant_aliases (
    user_email TEXT NOT NULL,
    original_name TEXT NOT NULL,
    primary_name TEXT NOT NULL,
    UNIQUE(user_email, original_name)
);
```

This is loaded into an in-memory cache at startup (alongside `user_aliases`). A call to `NormalizeName` checks the alias cache before falling back to contact resolution. Typical use: mapping informal names ("Boss", "CTO") to their canonical form for a specific tenant.

### 5.4 Reversed-Name Detection (sortedNameTokens)

`sortedNameTokens` canonicalizes a display name by **alphabetically sorting its tokens**:

```
"Phathit Chulothok" → tokens ["phathit", "chulothok"] → sorted → "chulothok phathit"
"Chulothok Phathit" → same sorted key
```

This key is used both in `GenerateTokenSortedProposals` (to propose merge candidates) and as a proposal-dedup index. Identifiers containing `@` or `+` (email, phone) bypass token-sorting since they are not name strings.

### 5.5 Multi-Channel Contact Save Paths

Each channel has a dedicated upsert path that handles the canonical_id format specific to that protocol:

| Function | canonical_id form | secondary_ids usage |
|---|---|---|
| `AutoUpsertContact` | email (lowercase) | none |
| `SaveWhatsAppContact` | phone number | email match appended as secondary |
| `SaveTelegramContact` | numeric user ID string | none |
| `UpsertContact` (generic) | caller-provided | via `aliases` param |

When a WhatsApp number matches an existing email-based contact by name (`handleWANameMatch`), the number is appended to `secondary_ids` via `appendSecondaryID`, and a new `contact_resolution` row is inserted pointing to the existing canonical root — no duplicate contact row is created.

---

## 6. Deduplication (Consolidate)

Message-level deduplication is detailed in → [08-services-business-logic.md]. This section covers only the identity-resolution interaction.

`services/consolidate.go` — `ConsolidateTasks` — merges AI-extracted `TodoItem` entries **within a single scan cycle** when they share the same `AffinityGroupID` and `AffinityScore >= 80`. This is pre-persistence deduplication, operating on in-memory `[]TodoItem` before any DB write.

The identity connection: two tasks from different channels are only candidates for consolidation if the **assignee string matches** after normalization. If the same person is named "John" in Slack and "john.smith@example.com" in Gmail and both identifiers have been resolved to the same canonical contact, the normalized assignee will match, enabling cross-channel consolidation.

Without alias resolution upstream of consolidation, cross-channel task merging silently fails — the affinity group may match, but mismatched assignee strings prevent the tasks from being surfaced as a single item.

---

## 7. Merge Workflow

### 7.1 Proposal Generation (Async)

```
POST /api/identity/proposals/generate  → 202 Accepted
GET  /api/identity/proposals/job-status → { status: "running|done|error|idle" }
GET  /api/identity/proposals           → []ProposalGroup
```

The async job (`runProposalJob`) runs three proposal strategies in sequence:

1. **AutoMerge** — deterministic, no user confirmation needed. Pairs with identical email canonical_ids are linked immediately via `LinkContact`. Returns a count of auto-merged pairs.

2. **AI proposals** — `IdentityResolver.ProposeGroups` analyzes the candidate contact list. Each `MergeGroup` is stored as rows in `identity_merge_candidates` via `InsertProposalGroup` using a shared `proposal_group_id` (random 16-char hex).

3. **Token-sorted proposals** — `GenerateTokenSortedProposals` detects reversed-name pairs (confidence 0.7) and unresolved message requester/assignee names that match existing contacts by sorted token key.

All three strategies respect `LoadHandledPairs`: if a pair's status is already `accepted` or `rejected`, it is skipped. This prevents re-surfacing decisions the user has already made.

### 7.2 User Confirm / Reject

```
POST /api/identity/proposals/:id/accept  { canonical_name: "John Smith" }
POST /api/identity/proposals/:id/reject
```

**Accept flow** (`AcceptProposalGroup`):
1. Collects all contact IDs in the proposal group.
2. Sorts IDs ascending; the lowest ID becomes the master.
3. Calls `LinkContact(ctx, tenantEmail, masterID, targetID)` for each subordinate.
4. If `canonical_name` is provided, overwrites `display_name` on the master contact.
5. Sets `status = 'accepted'` in `identity_merge_candidates`.

`LinkContact` is transactional:
- Sets `master_contact_id = masterID` on target.
- Calls `FlattenContactChildren` to re-point any of target's existing children directly to the new master (prevents two-hop chains).
- Copies `display_name` from target to master if master's is empty.
- Promotes `contact_type` via `PromoteContactType` (rank: internal > partner > customer > none).
- Inserts an `identity_merge_history` row with `reason = "Manual Link"`.
- Calls `GlobalContactDSU.Union(masterID, targetID)` to update in-memory state.
- After commit, runs `UpdateResolutionContactID` to redirect all `contact_resolution` rows pointing at targetID to masterID.

**Reject flow** (`RejectProposalGroup`): sets `status = 'rejected'`; no contact mutations occur. The pair is added to the handled set and will be skipped in future proposal jobs.

### 7.3 identity_merge_history (Audit Trail)

Every accepted link writes to `identity_merge_history`:

```sql
CREATE TABLE IF NOT EXISTS identity_merge_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_contact_id INTEGER NOT NULL REFERENCES contacts(id),
    target_contact_id INTEGER NOT NULL REFERENCES contacts(id),
    reason TEXT NOT NULL,
    merged_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

`reason` is currently always `"Manual Link"` for UI-confirmed merges. Future automated merges (e.g. AutoMergeByCanonicalID) could write distinct reasons here for audit traceability. The table is currently append-only; there is no unmerge-history path.

### 7.4 Unlink

`UnlinkContact` clears `master_contact_id` and calls `RebuildContactResolution` for the tenant. The DSU is **not updated** in-process after an unlink — it retains the stale Union until the next process restart (when `loadDSUFromDB` rebuilds from current DB state). This is an acceptable trade-off because unlinks are rare and infrequent; stale DSU entries only cause false-positive deduplication, which is the conservative error direction.

---

## 8. Known Limitations / Violation Cases

### ~~8.1 Phantom Type Violation: `EnrichedMessage.SenderID`~~ (해소됨 / Resolved)

**Resolved** — `refactor(types): promote EnrichedMessage.SenderID to ids.UserID phantom` 커밋으로 수정됨 (2026-05-01).

현재 `types/types.go`:

```go
SenderID ids.UserID `json:"sender_id"` // Why: Explicit phantom type for DB identity security.
```

`ids.UserID` (`type UserID int64`) phantom type으로 교체되어 CLAUDE.md "ID는 Phantom Type, 단순 `int64` 금지" 규칙을 준수한다.

### 8.2 DSU Not Updated on Unlink (In-Process)

As noted in §7.4: after `UnlinkContact`, `GlobalContactDSU` is not rebuilt. The in-memory graph retains the Union until process restart. This means that within the same process lifetime, a recently unlinked contact pair may still resolve to the same canonical root via `GlobalContactDSU.Find`, causing false-positive deduplication in `deduplicateByDSU`.

### 8.3 AI Proposal Chunking Does Not Cross Window Boundaries

`proposeInChunks` processes contacts in sequential non-overlapping windows of 20. A cross-window pair (contact 19 and contact 21) cannot be proposed by the AI in a single `proposeChunk` call. Token-sorted proposals partially compensate for this, but AI-only fuzzy matches that fall across a window boundary will be missed.

### 8.4 identity_merge_candidates Uses `contact_id_a < contact_id_b` Convention Implicitly

`InsertProposalGroup` enforces canonical ordering (`if a > b { a, b = b, a }`) before insert. `LoadHandledPairs` relies on the DB storing pairs with `contact_id_a < contact_id_b`. This invariant is maintained in code but not enforced by a DB CHECK constraint, making it a soft invariant.

---

## 9. Cross-References

| Topic | Chapter |
|---|---|
| Full schema for `contacts`, `contact_resolution`, `user_aliases` | → [04-data-layer.md] |
| AI filter pipeline (Gemini client, `generateWithRetry`, WhaTap APM instrumentation) | → [07-ai-filter-pipeline.md] |
| `HandleTaskState` and `routeTaskState` — where resolved assignee names are consumed | → [08-services-business-logic.md] |
| `EnrichedMessage` type definition (SenderID — `ids.UserID` phantom type) | → [02-domain-model.md] |
| Settings UI for managing contacts and proposals | → [14-frontend-settings.md] |
