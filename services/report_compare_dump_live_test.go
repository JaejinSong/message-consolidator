//go:build report_compare

package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"message-consolidator/config"
	"message-consolidator/store"
)

// Why: dumps the REAL report input payload (fetch + PrepareLogsForAI, sanitize skipped)
// so ai/report_compare_live_test.go can replay it against multiple models fairly.
// Run: set -a; source .env; set +a; REPORT_COMPARE_OUT=<dir> go test -tags=report_compare ./services/ -run TestReportCompare_DumpInput -v
func TestReportCompare_DumpInput(t *testing.T) {
	if os.Getenv("TURSO_DATABASE_URL") == "" {
		t.Skip("TURSO_DATABASE_URL not set — skipping report input dump")
	}
	outDir := os.Getenv("REPORT_COMPARE_OUT")
	if outDir == "" {
		t.Fatal("REPORT_COMPARE_OUT not set")
	}

	email := envDefault("REPORT_COMPARE_EMAIL", "jjsong@whatap.io")
	start := envDefault("REPORT_COMPARE_START", "2026-08-22")
	end := envDefault("REPORT_COMPARE_END", "2026-08-24")

	ctx := context.Background()
	cfg := config.LoadConfig()
	if err := store.InitDB(ctx, cfg); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	s := &ReportsService{config: ReportConfig{CutoffSize: DefaultReportCutoffSize}}
	activity, stalled, err := s.fetchAndFilterMessages(ctx, email, start, end, nil, nil)
	if err != nil {
		t.Fatalf("fetchAndFilterMessages: %v", err)
	}
	payload, truncated := s.PrepareLogsForAI(email, activity, stalled)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	inputPath := filepath.Join(outDir, "input.txt")
	if err := os.WriteFile(inputPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	windowPath := filepath.Join(outDir, "window.txt")
	if err := os.WriteFile(windowPath, []byte(start+" ~ "+end), 0o644); err != nil {
		t.Fatalf("write window: %v", err)
	}

	t.Logf("dumped %s: activity=%d stalled=%d bytes=%d truncated=%v window=%s ~ %s",
		inputPath, len(activity), len(stalled), len(payload), truncated, start, end)
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
