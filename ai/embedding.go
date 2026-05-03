package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"message-consolidator/internal/whataphttpx"
	"message-consolidator/logger"

	"github.com/whatap/go-api/trace"
	"google.golang.org/genai"
)

// DefaultEmbeddingModel is the production embedding model for archive semantic search.
// Why: text-embedding-004 isn't enabled on this API key — only gemini-embedding-001
// (Matryoshka, 3072d default). We request OutputDimensionality=768 so the wire format
// matches the slimmer 768d storage layout, getting the 4× transfer/store savings
// without paying for a model the project's API key cannot reach.
const (
	DefaultEmbeddingModel = "gemini-embedding-001"
	DefaultEmbeddingDim   = 768
	embedRequestTimeout   = 20 * time.Second

	taskTypeRetrievalDocument = "RETRIEVAL_DOCUMENT"
	taskTypeRetrievalQuery    = "RETRIEVAL_QUERY"
)

// outputDim is the int32 form of DefaultEmbeddingDim that the SDK config takes by
// pointer. Stored once so EmbedContentConfig can borrow it without per-call alloc.
var outputDim = func() *int32 { v := int32(DefaultEmbeddingDim); return &v }()

// EmbeddingClient owns a Gemini SDK handle scoped to embedding requests.
// Why: A separate type from GeminiClient lets us isolate embedding telemetry and
// rate limits without coupling them to the analysis/translation client.
type EmbeddingClient struct {
	client *genai.Client
	model  string
	dim    int
}

// NewEmbeddingClient constructs a Gemini-backed embedding client.
// model defaults to DefaultEmbeddingModel when empty so callers can leave the value
// unset for production usage.
func NewEmbeddingClient(ctx context.Context, apiKey, model string) (*EmbeddingClient, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("GEMINI_API_KEY is not set")
	}
	if model == "" {
		model = DefaultEmbeddingModel
	}
	// Why: ClientConfig.APIKey and HTTPClient are orthogonal in the new SDK —
	// the SDK injects the key via its own auth layer, so only a plain
	// WhaTap-wrapped client is needed (no apiKeyTransport shim required).
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:     apiKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: whataphttpx.Client(),
	})
	if err != nil {
		return nil, fmt.Errorf("genai client for embedding: %w", err)
	}
	logger.Infof("[EMBED] client ready (model=%s, dim=%d)", model, DefaultEmbeddingDim)
	return &EmbeddingClient{client: client, model: model, dim: DefaultEmbeddingDim}, nil
}

// Model returns the embedding model name in use, e.g. "gemini-embedding-001".
func (c *EmbeddingClient) Model() string { return c.model }

// Dim returns the dimensionality of vectors produced by Model().
func (c *EmbeddingClient) Dim() int { return c.dim }

// EmbedDocument produces a vector tuned for indexing the given text.
// Empty/whitespace-only text returns ErrEmptyText so the caller can skip the row.
func (c *EmbeddingClient) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	return c.embed(ctx, text, taskTypeRetrievalDocument, "Gemini-Embed-Doc")
}

// EmbedQuery produces a vector tuned for retrieval queries against documents
// embedded with EmbedDocument. Mixing the two task types degrades relevance.
func (c *EmbeddingClient) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return c.embed(ctx, text, taskTypeRetrievalQuery, "Gemini-Embed-Query")
}

// ErrEmptyText is returned when the input text contains no embeddable content.
var ErrEmptyText = errors.New("embedding input is empty")

func (c *EmbeddingClient) embed(ctx context.Context, text, taskType, stepName string) ([]float32, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrEmptyText
	}
	apiCtx, cancel := context.WithTimeout(ctx, embedRequestTimeout)
	defer cancel()

	start := time.Now()
	resp, err := c.client.Models.EmbedContent(apiCtx, c.model,
		[]*genai.Content{genai.NewContentFromText(text, genai.RoleUser)},
		&genai.EmbedContentConfig{TaskType: taskType, OutputDimensionality: outputDim},
	)
	elapsed := int(time.Since(start).Milliseconds())
	_ = trace.Step(ctx, stepName, "", elapsed, len(text))
	if err != nil {
		return nil, fmt.Errorf("embed content: %s", maskAPIKey(err))
	}
	if resp == nil || len(resp.Embeddings) == 0 || len(resp.Embeddings[0].Values) == 0 {
		return nil, errors.New("embed content: empty response")
	}
	return resp.Embeddings[0].Values, nil
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

	contents := make([]*genai.Content, 0, len(texts))
	indexMap := make([]int, 0, len(texts))
	for i, t := range texts {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		contents = append(contents, genai.NewContentFromText(t, genai.RoleUser))
		indexMap = append(indexMap, i)
	}
	if len(indexMap) == 0 {
		return make([][]float32, len(texts)), nil
	}

	start := time.Now()
	resp, err := c.client.Models.EmbedContent(apiCtx, c.model, contents,
		&genai.EmbedContentConfig{TaskType: taskTypeRetrievalDocument, OutputDimensionality: outputDim},
	)
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
