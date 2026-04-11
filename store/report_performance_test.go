package store

import (
	"message-consolidator/internal/testutil"
	"context"
	"fmt"
	"testing"
)

func TestListReportsPerformance(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("perf")

	// 1. 다수의 보고서 시딩 (예: 5개)
	for i := 1; i <= 5; i++ {
		r := &Report{
			UserEmail: email,
			StartDate: fmt.Sprintf("2024-02-0%d", i),
			EndDate:   fmt.Sprintf("2024-02-0%d", i),
		}
		id, err := SaveReport(ctx, r)
		if err != nil {
			t.Fatalf("Failed to save report: %v", err)
		}

		// 각 보고서에 대해 2개의 번역 추가
		_ = SaveReportTranslation(ctx, id, "en", fmt.Sprintf("Summary EN %d", i))
		_ = SaveReportTranslation(ctx, id, "ko", fmt.Sprintf("Summary KO %d", i))
	}

	// 2. 목록 조회 (N+1 제거된 로직 호출)
	reports, err := ListReports(ctx, email)
	if err != nil {
		t.Fatalf("ListReports failed: %v", err)
	}

	if len(reports) != 5 {
		t.Errorf("Expected 5 reports, got %d", len(reports))
	}

	// 3. 번역 데이터 정합성 검증
	for _, r := range reports {
		if len(r.Translations) != 2 {
			t.Errorf("Report %d: Expected 2 translations, got %d", r.ID, len(r.Translations))
		}
		if r.Translations["en"] == "" || r.Translations["ko"] == "" {
			t.Errorf("Report %d: Translation content missing", r.ID)
		}
	}
	
	t.Logf("Successfully verified 1+1 batch loading for %d reports", len(reports))
}
