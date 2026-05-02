package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"message-consolidator/internal/whataphttpx"
	"message-consolidator/logger"

	"github.com/google/generative-ai-go/genai"
	"github.com/whatap/go-api/trace"
	"google.golang.org/api/option"
)

// DefaultEmbeddingModel is the production embedding model for archive semantic search.
// Why: gemini-embedding-001 is the v1beta-compatible stable model for this API key
// (3072-dim). text-embedding-004 / embedding-001 are v1-only and return 404 on the
// genai Go SDK v0.13.0 which routes all requests through v1beta.
const (
	DefaultEmbeddingModel = "gemini-embedding-001"
	DefaultEmbeddingDim   = 3072
	embedRequestTimeout   = 20 * time.Second
)

// EmbeddingClient owns a Gemini SDK handle scoped to embedding requests.
// Why: GenerativeModel and EmbeddingModel are factory products of *genai.Client and
// share the same WhaTap-wrapped transport — keeping a separate type lets us isolate
// embedding telemetry and rate limits without touching GeminiClient.
type EmbeddingClient struct {
	client *genai.Client
	model  string
	dim    int
}

// NewEmbeddingClient constructs a Gemini-backed embedding client.
// model defaults to DefaultEmbeddingModel when empty so callers can leave the value
// unset for production usage.
func NewEmbeddingClient(ctx context.Context, apiKey, model string, opts ...option.ClientOption) (*EmbeddingClient, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("GEMINI_API_KEY is not set")
	}
	if model == "" {
		model = DefaultEmbeddingModel
	}
	allOpts := append([]option.ClientOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(whataphttpx.ClientWithAPIKey(apiKey)), //nolint:contextcheck // Per-request ctx passes through SDK calls.
	}, opts...)
	client, err := genai.NewClient(ctx, allOpts...)
	if err != nil {
		return nil, fmt.Errorf("genai client for embedding: %w", err)
	}
	logger.Infof("[EMBED] client ready (model=%s, dim=%d)", model, DefaultEmbeddingDim)
	return &EmbeddingClient{client: client, model: model, dim: DefaultEmbeddingDim}, nil
}

// Model returns the embedding model name in use, e.g. "text-embedding-004".
func (c *EmbeddingClient) Model() string { return c.model }

// Dim returns the dimensionality of vectors produced by Model().
func (c *EmbeddingClient) Dim() int { return c.dim }

// EmbedDocument produces a vector tuned for indexing the given text.
// Empty/whitespace-only text returns ErrEmptyText so the caller can skip the row.
func (c *EmbeddingClient) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	return c.embed(ctx, text, genai.TaskTypeRetrievalDocument, "Gemini-Embed-Doc")
}

// EmbedQuery produces a vector tuned for retrieval queries against documents
// embedded with EmbedDocument. Mixing the two task types degrades relevance.
func (c *EmbeddingClient) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return c.embed(ctx, text, genai.TaskTypeRetrievalQuery, "Gemini-Embed-Query")
}

// ErrEmptyText is returned when the input text contains no embeddable content.
var ErrEmptyText = errors.New("embedding input is empty")

func (c *EmbeddingClient) embed(ctx context.Context, text string, tt genai.TaskType, stepName string) ([]float32, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrEmptyText
	}
	apiCtx, cancel := context.WithTimeout(ctx, embedRequestTimeout)
	defer cancel()

	em := c.client.EmbeddingModel(c.model)
	em.TaskType = tt
	start := time.Now()
	resp, err := em.EmbedContent(apiCtx, genai.Text(text))
	elapsed := int(time.Since(start).Milliseconds())
	_ = trace.Step(ctx, stepName, "", elapsed, len(text))
	if err != nil {
		return nil, fmt.Errorf("embed content: %s", maskAPIKey(err))
	}
	if resp == nil || resp.Embedding == nil || len(resp.Embedding.Values) == 0 {
		return nil, errors.New("embed content: empty response")
	}
	return resp.Embedding.Values, nil
}

// EmbedDocumentBatch sends up to len(texts) document-typed embed requests in one
// HTTP round trip. Returns vectors aligned to texts; entries that the API rejected
// have nil at their slot — callers must skip nils. Empty texts are skipped before
// dispatch so the batch never carries empty parts (which the API rejects wholesale).
func (c *EmbeddingClient) EmbedDocumentBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	apiCtx, cancel := context.WithTimeout(ctx, embedRequestTimeout*2)
	defer cancel()

	em := c.client.EmbeddingModel(c.model)
	em.TaskType = genai.TaskTypeRetrievalDocument
	batch := em.NewBatch()
	indexMap := make([]int, 0, len(texts))
	for i, t := range texts {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		batch.AddContent(genai.Text(t))
		indexMap = append(indexMap, i)
	}
	if len(indexMap) == 0 {
		return make([][]float32, len(texts)), nil
	}

	start := time.Now()
	resp, err := em.BatchEmbedContents(apiCtx, batch)
	_ = trace.Step(ctx, "Gemini-Embed-Batch", "", int(time.Since(start).Milliseconds()), len(indexMap))
	if err != nil {
		return nil, fmt.Errorf("batch embed: %s", maskAPIKey(err))
	}
	if resp == nil || len(resp.Embeddings) != len(indexMap) {
		return nil, errors.New("batch embed: response length mismatch")
	}

	out := make([][]float32, len(texts))
	for i, emb := range resp.Embeddings {
		if emb == nil || len(emb.Values) == 0 {
			continue
		}
		out[indexMap[i]] = emb.Values
	}
	return out, nil
}
