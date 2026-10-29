# Filter Feedback Loop (Clef noise-filter threshold calibration)

Status: approved by user 2026-10-19. Branch: `claude/filter-feedback-loop`. No deploy in this branch run.

## Background
- Noise pre-filter `ai/clef_filter.go` (Clef decision model, Ollama cloud `/v1/systemone`) is live with
  `FILTER_PROVIDER=clef`. Message is noise iff `P(actionable) < threshold` (env `CLEF_THRESHOLD`, default 0.3).
  Legacy LLM filter `ai/filter_service.go` stays untouched (do not edit it or `lite_filter.prompt`).
- Today the score is discarded; only drops are written to `extraction_decisions` (240-rune head, no
  room/source_ts/message id, no score). No UI reads it. Filtered messages can never be recovered.
- Chat channels filter a whole group payload (`scanner/channel_adapter.go` ~255/266), Gmail filters per
  message (`channels/gmail_scan.go` `filterGmailBatch`).
- Labeled eval (260 rows): Clef@0.3 dropped 3/85 real tasks, caught 72% of known noise.
  Clef@0.2 dropped 0/85, caught 59%. Clef@0.5 dropped 9/85, caught 87%.

## Goal
Learn the threshold from feedback. Missing a real task is far worse than passing noise, so the
threshold moves DOWN fast and UP slowly, always within [0.1, 0.5].

## Signals
| Signal | Meaning | Source |
|---|---|---|
| passed, extraction produced no task | false pass (noise got through) | extraction outcome linkage (Step 2) |
| passed, resulting undone task deleted by user | false pass | deletion hook (Step 4) |
| dropped, user clicks "was a task" | false drop | review UI (Step 3) |
| dropped, user manually adds a matching task | false drop (approx) | manual-add hook (Step 4) |
| borderline drop confirmed in digest | either | daily digest (Step 6) |

## Data model (Step 1 owns ALL schema + sqlc queries; later steps must not touch store/queries or db/)
New table `filter_scores` (next schema version, migration in `store/migrations.go` style):
- `id INTEGER PK AUTOINCREMENT`, `user_email TEXT NOT NULL`, `source TEXT NOT NULL`, `room TEXT NOT NULL DEFAULT ''`,
  `source_ts TEXT NOT NULL DEFAULT ''` (latest message ts in the group / Gmail message ts),
  `message_ref TEXT NOT NULL DEFAULT ''` (Gmail message id; chat: comma-joined message ids or source_ts list),
  `provider TEXT NOT NULL` ('clef'), `p_actionable REAL NOT NULL`, `threshold REAL NOT NULL`,
  `verdict TEXT NOT NULL` ('pass'|'noise'), `category TEXT NOT NULL DEFAULT ''`,
  `text TEXT NOT NULL DEFAULT ''` (max 8000 runes), `outcome TEXT NOT NULL DEFAULT ''`
  ('' | 'task' | 'none' | 'rescued' | 'confirmed_noise' | 'deleted_noise' | 'manual_match'),
  `outcome_at DATETIME`, `created_at DATETIME DEFAULT CURRENT_TIMESTAMP`.
- Indexes: `(user_email, created_at)`, `(user_email, source, room, source_ts)`, `(user_email, verdict, outcome)`.
- Retention: 31 days, pruned by a daily job.

sqlc queries (store/queries/filter_scores.sql), all user-scoped:
- `InsertFilterScore` (returns id)
- `GetFilterScore(id, user_email)`
- `ListFilterDrops(user_email, since, limit)` newest first, verdict='noise'
- `ListBorderlineDrops(user_email, since, lo, hi, limit)` verdict='noise' AND outcome='' AND p in [lo,hi) ORDER BY p DESC
- `SetFilterOutcome(id, user_email, outcome)` sets outcome_at=now
- `LinkFilterOutcomeBySourceTS(user_email, source, room, source_ts, outcome)` only where verdict='pass' AND outcome=''
- `ListFilterScoresSince(user_email, since)` for calibration
- `ListFilterUsers(since)` distinct user_email
- `DeleteFilterScoresBefore(cutoff)`

## Filter interface (Step 1)
- New `ai.FilterInput{Email, Source, Room, SourceTS, MessageRef, Text string}`.
- Consumer interfaces (`scanner` noiseFilter, `channels` noiseFilter) change to `IsNoise(ctx, ai.FilterInput) (bool, error)`.
  Both `ClefFilter` and `LiteFilter` implement it (LiteFilter: thin adapter method that calls the existing
  logic unchanged; do not alter its prompt/parse behaviour).
- ClefFilter writes one `filter_scores` row per successful call (pass AND noise) with p, threshold, verdict, category,
  text capped at 8000 runes. Keep the existing `extraction_decisions` write on noise. A failed insert is logged, never
  changes the verdict.
- Callers pass Room/SourceTS/MessageRef where readily available (Gmail: message id + ts; chat: room + latest ts).

## Step 2: outcome linkage (auto false-pass signal)
- When extraction for a passed payload finishes, link outcome: any new task created → 'task'; otherwise 'none'.
  Use `LinkFilterOutcomeBySourceTS`. Files: `services/task_routing.go`, `scanner/channel_adapter.go`, `channels/gmail_scan.go`.
- Must not change extraction behaviour. Fire-and-forget errors are logged.

## Step 3: review UI + rescue (false-drop signal + recovery)
- API (handlers, auth-required, user-scoped):
  - `GET /api/filter/drops?days=7` → recent drops (id, source, room, p, category, text head ≤ 400 runes, created_at, outcome).
  - `POST /api/filter/drops/rescue {id}` → outcome='rescued', then re-run extraction on the stored text via the
    `CompletionService.fallbackToNewExtraction`-style path (Analyze + HandleTaskState) bypassing the filter. Gmail: also
    clear processed state only if needed for consistency. Idempotent (second rescue is a no-op).
  - `POST /api/filter/drops/confirm {id}` → outcome='confirmed_noise'.
- Frontend: a "걸러진 메시지" section in the Learning tab (`src/renderers/learning-renderer.ts`, `src/api.ts`), list with
  "태스크였음" / "노이즈 맞음" buttons. Korean UI strings via existing i18n pattern. BEM, rem/variables.css tokens, no `any`.

## Step 4: implicit signals from existing correction hooks
- `RecordTaskDeletion` (undone task deleted): find the passed `filter_scores` row by (source, room, source_ts) → outcome='deleted_noise'.
- `RecordManualAdd`: look for a noise row of the same user in the last 7 days whose text shares ≥ 3 significant tokens with
  the manual task text (reuse existing tokenisation in correction learning) → outcome='manual_match'. Best effort.
- File: `services/correction_learning.go` (+ test). Fire-and-forget, never blocks the user action.

## Step 5: calibrator + per-user threshold
- Threshold lookup: `app_settings` key `clef_threshold:<email>`; fallback to env `CLEF_THRESHOLD`. 61 s cache in ClefFilter.
- Daily job (`primeLoop` in `scanner/scanner.go`, modelled on `precision-observer`), per user over the last 31 days:
  - false drops = outcomes 'rescued' + 'manual_match'. If ≥ 1: proposal = max(0.1, min(p of false drops) - 0.05).
  - else if no false drops for 31 days AND false-pass rate ('none' + 'deleted_noise') / labeled passes ≥ 0.4 with ≥ 29
    labeled passes: proposal = min(0.5, current + 0.05).
  - else no proposal.
- Proposal is confirm-first: a `correction_observations` row kind `filter_threshold` (from_value=current, to_value=proposal,
  evidence summary). Approve in the Learning tab (existing `DecideObservation` flow) writes the `app_settings` key.
  Make sure `filter_threshold` is excluded from G6 suppress rules like `precision` is.
- Also run `DeleteFilterScoresBefore(now-31d)` in the same daily job.

## Step 6: digest active labeling
- Daily digest (existing service) appends up to 3 borderline drops (p in [threshold-0.1, threshold), outcome='') with a
  link to the Learning tab review section. No new interactive Slack actions.

## Non-goals
- No change to `lite_filter.prompt` / `ai/filter_service.go` behaviour. No deploy. No automatic threshold apply.
