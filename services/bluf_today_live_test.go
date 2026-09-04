//go:build report_ab

package services

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"message-consolidator/ai"
)

// TestBLUFToday answers "what would the BLUF be right now?" against the live database: it
// builds the daily and weekly candidate rankings exactly as processAsyncReport would and, when
// DEEPSEEK_API_KEY is present, runs the real panel + judge on each. Read-only; skips without
// REPORT_AB_EMAIL. Identity resolution is not applied (that needs store.InitDB), so party
// categories fall back to MapContactType's domain rule -- the same fallback production uses
// for unresolved contacts.
func TestBLUFToday(t *testing.T) {
	loadABEnv(t)
	email := envOrSkip(t, "REPORT_AB_EMAIL")
	db, err := openReadOnlyDB()
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	rows, err := db.QueryContext(ctx, abInputQuery, email)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	var all []Log
	for rows.Next() {
		m, err := scanABLog(rows)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		all = append(all, m)
	}
	rows.Close()

	var client *ai.AIClient
	if key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")); key != "" {
		client, err = ai.NewAIClient(ctx, ai.ProviderConfig{Provider: "deepseek", DeepSeekAPIKey: key, DeepSeekBaseURL: os.Getenv("DEEPSEEK_BASE_URL")})
		if err != nil {
			t.Fatalf("NewAIClient: %v", err)
		}
	}

	today := time.Now().Format("2006-01-02")
	weekStart := time.Now().AddDate(0, 0, -6).Format("2006-01-02")
	for _, w := range []struct{ name, start, end string }{{"daily", today, today}, {"weekly", weekStart, today}} {
		activity := withinWindow(all, w.start, w.end)
		var backlog []Log
		for _, m := range all {
			if !m.Done && m.CreatedAt.Format("2006-01-02") < w.start {
				backlog = append(backlog, m)
			}
		}
		dossiers := buildBLUFDossiers(activity, backlog, email, time.Now())
		var sb strings.Builder
		for i, d := range dossiers {
			fmt.Fprintf(&sb, "\n  #%-2d %3d  id=%d  %s\n        [%s]", i+1, d.score, d.log.ID, truncateRunes(d.log.Task, 70), strings.Join(d.signals, "; "))
		}
		t.Logf("=== %s %s ~ %s: activity=%d backlog=%d candidates=%d%s", w.name, w.start, w.end, len(activity), len(backlog), len(dossiers), sb.String())
		if client == nil || len(dossiers) == 0 {
			continue
		}
		start := time.Now()
		res, err := client.SelectBLUF(ctx, email, renderBLUFDossiers(dossiers, email), w.start+" ~ "+w.end, 0)
		if err != nil {
			t.Errorf("%s SelectBLUF: %v", w.name, err)
			continue
		}
		t.Logf(">>> %s BLUF (%s, %d drafts, %d words): %s\n    covers=%v\n    rationale=%s\n    pattern=%s\n    lead=%s",
			w.name, time.Since(start).Round(time.Second), res.Nominations, len(strings.Fields(res.Line)), res.Line, res.CandidateIDs, res.Rationale, res.Pattern, res.LeadStake)
	}
}
