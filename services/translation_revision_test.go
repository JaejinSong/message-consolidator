package services

import (
	"context"
	"fmt"
	"io"
	"message-consolidator/ai"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/genai"
)

func TestBatchTranslationKeyIncludesSourceAndTenant(t *testing.T) {
	original := []store.TranslateRequest{{ID: 1, Text: `{"t":"A","s":["first"]}`}}
	changed := []store.TranslateRequest{{ID: 1, Text: `{"t":"A","s":["second"]}`}}
	key := batchTranslationKey("alice@example.com", original, "ko")
	if key == batchTranslationKey("alice@example.com", changed, "ko") {
		t.Error("different source texts share a flight")
	}
	if key == batchTranslationKey("bob@example.com", original, "ko") {
		t.Error("different tenants share a flight")
	}
	if key == batchTranslationKey("alice@example.com", original, "en") {
		t.Error("different languages share a flight")
	}
	if key != batchTranslationKey("alice@example.com", original, "ko") {
		t.Error("identical requests have different keys")
	}
}

func TestTranslateBatchConcurrentDifferentSourcesStaySeparate(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	client, err := ai.NewGeminiClient(ctx, "test-key", "test-model", "test-model", func(cfg *genai.ClientConfig) {
		cfg.HTTPClient = &http.Client{Transport: translationRoundTrip(func(r *http.Request) (*http.Response, error) {
			call := calls.Add(1)
			started <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			body := fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[{"text":"[{\"message_id\":1,\"translated_text\":\"call %d\"}]"}]},"finishReason":"STOP"}]}`, call)
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewTranslationService(client)
	completed := make(chan []ai.TranslationResult, 2)
	go func() {
		result, _ := svc.TranslateBatch(ctx, "user@example.com", []store.TranslateRequest{{ID: 1, Text: "A"}}, "ko")
		completed <- result
	}()
	select {
	case <-started:
	case <-ctx.Done():
		close(release)
		t.Fatal("first request did not start")
	}
	go func() {
		result, _ := svc.TranslateBatch(ctx, "user@example.com", []store.TranslateRequest{{ID: 1, Text: "B"}}, "ko")
		completed <- result
	}()
	separate := true
	select {
	case <-started:
	case <-ctx.Done():
		separate = false
	}
	close(release)
	first, second := <-completed, <-completed
	if !separate || calls.Load() != 2 {
		t.Fatalf("different sources shared a flight: calls=%d", calls.Load())
	}
	if len(first) != 1 || len(second) != 1 || first[0].Text == second[0].Text {
		t.Fatalf("different sources shared response: %v, %v", first, second)
	}
}

type translationRoundTrip func(*http.Request) (*http.Response, error)

func (f translationRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestJITTranslationRejectsLateSourceAndKeepsValidPartial(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	ctx := context.Background()
	email := testutil.RandomEmail("jit-revision")
	ids := make([]store.MessageID, 2)
	for i, title := range []string{"A", "Stable"} {
		result, err := store.GetDB().ExecContext(ctx, "INSERT INTO messages(user_email, task, source, source_ts) VALUES(?, ?, 'slack', ?)", email, title, testutil.RandomTS(title))
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		ids[i] = store.MessageID(id)
	}
	client, err := ai.NewGeminiClient(ctx, "test-key", "test-model", "test-model", func(cfg *genai.ClientConfig) {
		cfg.HTTPClient = &http.Client{Transport: translationRoundTrip(func(r *http.Request) (*http.Response, error) {
			if _, err := store.GetDB().ExecContext(ctx, "UPDATE messages SET task='B' WHERE id=?", ids[0]); err != nil {
				return nil, err
			}
			if err := store.SaveTaskTranslationsBulk(ctx, "ko", map[store.MessageID]string{ids[0]: "B translation"}); err != nil {
				return nil, err
			}
			body := fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[{"text":"[{\"message_id\":%d,\"translated_text\":\"late A\"},{\"message_id\":%d,\"translated_text\":\"stable translation\"}]"}]},"finishReason":"STOP"}]}`, ids[0], ids[1])
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewTasksService(NewTranslationService(client), nil)
	results, err := service.ProcessBatchTranslation(ctx, email, ids, "ko")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results: %+v", results)
	}
	if results[0].Success || results[0].TranslatedText != "" {
		t.Errorf("late source succeeded: %+v", results[0])
	}
	if !results[1].Success || results[1].TranslatedText != "stable translation" {
		t.Errorf("valid partial lost: %+v", results[1])
	}
	cached, err := store.GetTaskTranslationsBatch(ctx, ids, "ko")
	if err != nil || cached[ids[0]] != "B translation" {
		t.Fatalf("new source overwritten: %v, %v", cached, err)
	}
}

func TestTranslationRequestsRejectForeignMessage(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	ctx := context.Background()
	result, err := store.GetDB().ExecContext(ctx, "INSERT INTO messages(user_email, task, source, source_ts) VALUES(?, 'Private title', 'slack', ?)", testutil.RandomEmail("owner"), testutil.RandomTS("private"))
	if err != nil {
		t.Fatal(err)
	}
	rawID, _ := result.LastInsertId()
	id := store.MessageID(rawID)
	svc := NewTasksService(NewTranslationService(nil), nil)
	reqs, sources := svc.PrepareTranslateRequestsWithSources(ctx, "other@example.com", []store.MessageID{id}, nil)
	if len(reqs) != 0 || len(sources) != 0 {
		t.Fatalf("foreign content reached translation: %v, %v", reqs, sources)
	}
}

func TestJITRejectsForeignCachedTranslation(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	ctx := context.Background()
	result, err := store.GetDB().ExecContext(ctx, "INSERT INTO messages(user_email, task, source, source_ts) VALUES(?, 'Private title', 'slack', ?)", testutil.RandomEmail("owner"), testutil.RandomTS("private"))
	if err != nil {
		t.Fatal(err)
	}
	rawID, _ := result.LastInsertId()
	id := store.MessageID(rawID)
	if err := store.SaveTaskTranslationsBulk(ctx, "ko", map[store.MessageID]string{id: "Private translation"}); err != nil {
		t.Fatal(err)
	}
	svc := NewTasksService(NewTranslationService(nil), nil)
	results, err := svc.ProcessBatchTranslation(ctx, "other@example.com", []store.MessageID{id}, "ko")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Success || results[0].TranslatedText != "" {
		t.Fatalf("foreign translation leaked: %+v", results)
	}
}
