package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"message-consolidator/internal/testutil"
	"message-consolidator/services"
	"message-consolidator/store"
)

func seedFilterDrop(t *testing.T, email, verdict string) int64 {
	t.Helper()
	id, err := store.InsertFilterScore(context.Background(), store.FilterScoreInput{
		UserEmail: email, Source: store.SourceSlack, Room: "r", SourceTS: "1.1", Provider: "clef",
		PActionable: 0.2, Threshold: 0.3, Verdict: verdict, Text: "ship the release",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return id
}

func filterReq(method, url, email string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, url, &buf)
	return req.WithContext(WithMockUser(req.Context(), email))
}

func TestFilterReviewHandlers(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	owner := testutil.RandomEmail("fr-owner")
	other := testutil.RandomEmail("fr-other")
	dropID := seedFilterDrop(t, owner, store.FilterVerdictNoise)
	passID := seedFilterDrop(t, owner, store.FilterVerdictPass)
	api := &API{FilterReview: services.NewFilterReviewService(nil)}

	t.Run("list returns only the caller's drops", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleListFilterDrops(rr, filterReq("GET", "/api/filter/drops?days=7", owner, nil))
		var got []services.FilterDrop
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &got) != nil || len(got) != 1 || got[0].ID != dropID {
			t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
		}
		rr = httptest.NewRecorder()
		api.HandleListFilterDrops(rr, filterReq("GET", "/api/filter/drops", other, nil))
		if rr.Body.String() != "[]" {
			t.Errorf("other user body = %s", rr.Body.String())
		}
	})

	t.Run("confirm other user's row is 404", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleConfirmFilterDrop(rr, filterReq("POST", "/x", other, map[string]int64{"id": dropID}))
		if rr.Code != http.StatusNotFound {
			t.Errorf("code = %d", rr.Code)
		}
	})

	t.Run("confirm a pass row is 400", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleConfirmFilterDrop(rr, filterReq("POST", "/x", owner, map[string]int64{"id": passID}))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("code = %d", rr.Code)
		}
	})

	t.Run("invalid id is 400", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleRescueFilterDrop(rr, filterReq("POST", "/x", owner, map[string]int64{"id": 0}))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("code = %d", rr.Code)
		}
	})

	t.Run("rescue without AI is 503 and leaves the drop untouched", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleRescueFilterDrop(rr, filterReq("POST", "/x", owner, map[string]int64{"id": dropID}))
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("code = %d", rr.Code)
		}
	})

	t.Run("confirm marks confirmed_noise", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleConfirmFilterDrop(rr, filterReq("POST", "/x", owner, map[string]int64{"id": dropID}))
		row, _ := store.GetFilterScore(context.Background(), owner, dropID)
		if rr.Code != 200 || row.Outcome != store.FilterOutcomeConfirmedNoise {
			t.Errorf("code=%d outcome=%q", rr.Code, row.Outcome)
		}
	})
}
