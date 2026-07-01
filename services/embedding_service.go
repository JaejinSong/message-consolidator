package services

import (
	"context"
	"crypto/sha1" //nolint:gosec // Why: hash used only for change detection, not security.
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/store"

	"github.com/whatap/go-api/method"
	"github.com/whatap/go-api/trace"
)

// Embedder is the consumer-defined interface this service depends on.
// Why: keeps services package independent of the ai package's concrete client and
// lets tests substitute a stub without spinning up a Gemini transport.
type Embedder interface {
	EmbedDocument(ctx context.Context, text string) ([]float32, error)
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
	Model() string
	Dim() int
}

// EmbeddingService coordinates embedding generation, hybrid retrieval, and backfill.
type EmbeddingService struct {
	client Embedder

	// per-message embed timeout for fire-and-forget enqueues
	enqueueTimeout time.Duration

	// fts/sem candidate caps & RRF tunable
	ftsCandidates int
	semCandidates int
	rrfK          int
}

// Tunables for the hybrid ranker.
const (
	defaultFTSCandidates  = 100
	defaultSemCandidates  = 100
	defaultRRFK           = 60
	defaultEnqueueTimeout = 30 * time.Second
)

// NewEmbeddingService builds an EmbeddingService with production defaults.
func NewEmbeddingService(client Embedder) *EmbeddingService {
	return &EmbeddingService{
		client:         client,
		enqueueTimeout: defaultEnqueueTimeout,
		ftsCandidates:  defaultFTSCandidates,
		semCandidates:  defaultSemCandidates,
		rrfK:           defaultRRFK,
	}
}

// EnqueueForMessage embeds a single message in the background. Caller's context
// is detached so the request handler can return immediately; a fresh timeout
// keeps the embed call from leaking goroutines if the API stalls.
func (s *EmbeddingService) EnqueueForMessage(_ context.Context, msgID store.MessageID) {
	if s == nil || s.client == nil {
		return
	}
	go func() {
		defer safego.Recover("embed-enqueue")
		ctx, cancel := context.WithTimeout(context.Background(), s.enqueueTimeout)
		defer cancel()
		ctx2, _ := trace.Start(ctx, "/embed.EnqueueForMessage")
		var err error
		defer func() { _ = trace.End(ctx2, err) }()
		err = s.embedAndStore(ctx2, msgID)
		if err != nil {
			logger.Warnf("[EMBED] enqueue id=%d failed: %v", msgID, err)
		}
	}()
}

// embedAndStore loads the message text, computes the embedding, and persists it.
// Skips work when the stored hash already matches (text unchanged + same model).
func (s *EmbeddingService) embedAndStore(ctx context.Context, msgID store.MessageID) error {
	msg, err := store.GetMessageByID(ctx, store.GetDB(), "", msgID)
	if err != nil {
		return fmt.Errorf("load msg %d: %w", msgID, err)
	}
	hash := embeddingTextHash(msg.Task, msg.OriginalText)
	if existing, prevHash, _, ok, err := store.GetEmbedding(ctx, msgID); err == nil && ok && prevHash == hash && len(existing) > 0 {
		return nil
	}
	combined := joinForEmbedding(msg.Task, msg.OriginalText)
	vec, err := s.client.EmbedDocument(ctx, combined)
	if err != nil {
		return fmt.Errorf("embed doc: %w", err)
	}
	return store.UpsertEmbedding(ctx, msgID, s.client.Model(), s.client.Dim(), Float32sToBytes(vec), hash)
}

// BackfillBatch fetches up to `batch` archive rows missing or stale for the
// configured model and embeds them. Returns counts so the caller can drive a
// progress UI or cron loop. Why: bounded batch keeps Gemini quota predictable
// and lets ops abort/resume cleanly.
func (s *EmbeddingService) BackfillBatch(ctx context.Context, email string, batch int) (processed, skipped, failed int, err error) {
	if s == nil || s.client == nil {
		return 0, 0, 0, errors.New("embedding service not ready")
	}
	if batch <= 0 {
		batch = 100
	}
	rows, err := store.ListMissingEmbeddings(ctx, email, s.client.Model(), batch)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, r := range rows {
		combined := joinForEmbedding(r.Task, r.OriginalText)
		if combined == "" {
			skipped++
			continue
		}
		vec, e := s.client.EmbedDocument(ctx, combined)
		if e != nil {
			failed++
			logger.Warnf("[EMBED] backfill id=%d: %v", r.ID, e)
			continue
		}
		hash := embeddingTextHash(r.Task, r.OriginalText)
		if e := store.UpsertEmbedding(ctx, r.ID, s.client.Model(), s.client.Dim(), Float32sToBytes(vec), hash); e != nil {
			failed++
			logger.Warnf("[EMBED] backfill upsert id=%d: %v", r.ID, e)
			continue
		}
		processed++
	}
	return processed, skipped, failed, nil
}

// CountMissing reports how many archive messages still need embeddings.
func (s *EmbeddingService) CountMissing(ctx context.Context, email string) (int, error) {
	if s == nil || s.client == nil {
		return 0, errors.New("embedding service not ready")
	}
	return store.CountMissingEmbeddings(ctx, email, s.client.Model())
}

// SearchHybrid runs FTS5 BM25 ∪ cosine top-K, fuses ranks with RRF, then resolves
// the surviving IDs into ConsolidatedMessage rows. Why: BM25 captures exact name/
// IP/code matches; cosine captures paraphrase + cross-language meaning. RRF gives
// each side equal voice without tuning a heuristic weight.
func (s *EmbeddingService) SearchHybrid(ctx context.Context, email, query string, limit int) ([]store.ConsolidatedMessage, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("embedding service not ready")
	}
	mctx, _ := method.Start(ctx, "embed.SearchHybrid")
	var err error
	defer func() { _ = method.End(mctx, err) }()

	if limit <= 0 {
		limit = 50
	}

	ftsIDs, ferr := store.ArchiveFTSTopIDs(ctx, email, query, s.ftsCandidates)
	if ferr != nil {
		// Why: lexical fallback shouldn't kill the whole search. Log and continue
		// with semantic-only ranking — pure cosine is still a useful result.
		logger.Warnf("[EMBED] fts side failed: %v", ferr)
		ftsIDs = nil
	}

	qvec, qerr := s.client.EmbedQuery(ctx, query)
	var semIDs []store.MessageID
	if qerr != nil {
		// Why: embed transient failure → degrade to FTS-only instead of failing the user.
		logger.Warnf("[EMBED] query embed failed: %v", qerr)
	} else {
		qvJSON := vectorToJSON(qvec)
		scored, semErr := store.SemanticTopK(ctx, email, s.client.Model(), qvJSON, s.semCandidates)
		if semErr != nil {
			logger.Warnf("[EMBED] semantic top-k failed: %v", semErr)
		} else {
			semIDs = make([]store.MessageID, len(scored))
			for i, sc := range scored {
				semIDs[i] = sc.ID
			}
		}
	}

	if len(ftsIDs) == 0 && len(semIDs) == 0 {
		return nil, nil
	}

	fused := rrfFuse(ftsIDs, semIDs, s.rrfK, limit)
	if len(fused) == 0 {
		return nil, nil
	}
	msgs, err := store.GetMessagesByIDs(ctx, store.GetDB(), email, fused)
	if err != nil {
		return nil, fmt.Errorf("resolve fused ids: %w", err)
	}
	return reorderByIDs(msgs, fused), nil
}

// OpenTaskCandidate pairs an open task with its cosine similarity to the query.
type OpenTaskCandidate struct {
	Task  store.ConsolidatedMessage
	Score float32
}

// CandidateOpenTasks embeds queryText and returns the top-k OPEN tasks ranked by
// cosine similarity. Why: cross-channel completion matching needs the still-open task
// that a "done" message resolves — SearchHybrid targets the archive, so this uses the
// open-task variant (SemanticTopKOpen) and resolves IDs preserving rank order.
func (s *EmbeddingService) CandidateOpenTasks(ctx context.Context, email, queryText string, k int) ([]OpenTaskCandidate, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("embedding service not ready")
	}
	if strings.TrimSpace(queryText) == "" || k <= 0 {
		return nil, nil
	}
	vec, err := s.client.EmbedQuery(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	scored, err := store.SemanticTopKOpen(ctx, email, s.client.Model(), vectorToJSON(vec), k)
	if err != nil {
		return nil, err
	}
	if len(scored) == 0 {
		return nil, nil
	}
	ids := make([]store.MessageID, len(scored))
	scoreByID := make(map[store.MessageID]float32, len(scored))
	for i, sc := range scored {
		ids[i] = sc.ID
		scoreByID[sc.ID] = sc.Score
	}
	msgs, err := store.GetMessagesByIDs(ctx, store.GetDB(), email, ids)
	if err != nil {
		return nil, fmt.Errorf("resolve open candidate ids: %w", err)
	}
	ordered := reorderByIDs(msgs, ids)
	out := make([]OpenTaskCandidate, 0, len(ordered))
	for _, m := range ordered {
		out = append(out, OpenTaskCandidate{Task: m, Score: scoreByID[m.ID]})
	}
	return out, nil
}

// vectorToJSON encodes a float32 slice as a JSON array string — the format
// libsql's vector32() accepts as a bind parameter for DB-side cosine computation.
func vectorToJSON(v []float32) string {
	var sb strings.Builder
	sb.Grow(8 * len(v))
	sb.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// rrfFuse implements Reciprocal Rank Fusion over two ranked ID lists.
// score(id) = Σ 1/(k + rank_i). Output is the top `limit` IDs by fused score.
// Why: classic RRF (k=60) is parameter-light and outperforms naive score
// addition when the two scorers come from different distributions.
func rrfFuse(fts, sem []store.MessageID, k, limit int) []store.MessageID {
	scores := make(map[store.MessageID]float64, len(fts)+len(sem))
	for i, id := range fts {
		scores[id] += 1.0 / float64(k+i+1)
	}
	for i, id := range sem {
		scores[id] += 1.0 / float64(k+i+1)
	}
	type pair struct {
		id store.MessageID
		s  float64
	}
	pairs := make([]pair, 0, len(scores))
	for id, sc := range scores {
		pairs = append(pairs, pair{id, sc})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].s != pairs[j].s {
			return pairs[i].s > pairs[j].s
		}
		// Why: stable tiebreaker keeps pagination behavior deterministic.
		return pairs[i].id < pairs[j].id
	})
	if limit > len(pairs) {
		limit = len(pairs)
	}
	out := make([]store.MessageID, limit)
	for i := 0; i < limit; i++ {
		out[i] = pairs[i].id
	}
	return out
}

// reorderByIDs returns msgs sorted to follow the order of ids, dropping any
// whose ID isn't in ids. Why: GetMessagesByIDs returns rows in DB order, but
// the caller wants RRF rank order preserved.
func reorderByIDs(msgs []store.ConsolidatedMessage, ids []store.MessageID) []store.ConsolidatedMessage {
	byID := make(map[store.MessageID]store.ConsolidatedMessage, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
	}
	out := make([]store.ConsolidatedMessage, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			out = append(out, m)
		}
	}
	return out
}

// joinForEmbedding combines task title + original text with a separator the
// embedding model can lean on as a soft section boundary.
func joinForEmbedding(task, original string) string {
	switch {
	case task != "" && original != "":
		return task + "\n\n" + original
	case task != "":
		return task
	default:
		return original
	}
}

// embeddingTextHash is a hex SHA-1 of (task ⟂ original_text) used to detect text
// drift so backfill can skip rows whose stored vector is still current.
func embeddingTextHash(task, original string) string {
	h := sha1.New() //nolint:gosec // Why: change detection only.
	h.Write([]byte(task))
	h.Write([]byte{0x1f})
	h.Write([]byte(original))
	return hex.EncodeToString(h.Sum(nil))
}

// Float32sToBytes packs a vector as little-endian float32 BLOB.
func Float32sToBytes(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// BytesToFloat32s decodes a little-endian float32 BLOB. Returns nil if the
// length isn't a multiple of 4 (corrupt row).
func BytesToFloat32s(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := 0; i < len(out); i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}


