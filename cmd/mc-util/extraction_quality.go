package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"message-consolidator/config"
)

// runExtractionQuality prints the aggregates that judge extraction precision.
//
// Why: the user's own triage is the only ground truth this system has -- a cancelled task
// (done=0, is_deleted=1) is a labelled extraction error. Those labels were only ever read
// by hand, which is how four prompt changes shipped on 2026-09-10 before anything could
// measure them. This turns the measurement half of the loop into something repeatable.
//
// It also reports learning-loop health, because the automated improve half has a failure
// mode that is invisible from the outside: suppress observations key on a sorted token bag
// of the message, so they only accumulate on a near-identical repeat. On 2026-09-10 all 49
// of them sat at evidence_count=1 with none promoted, while 726 cancellations went unused.
func runExtractionQuality(cfg *config.Config) {
	fs := flag.NewFlagSet("extraction-quality", flag.ExitOnError)
	since := fs.String("since", "", "only count tasks created at or after this UTC timestamp (e.g. 2026-09-10 15:25:00)")
	email := fs.String("email", "", "tenant to report on (default: DEFAULT_USER_EMAIL, else jjsong@whatap.io)")
	_ = fs.Parse(os.Args[2:])

	if cfg.TursoURL == "" {
		log.Fatal("TURSO_DATABASE_URL is not set")
	}
	dbURL := cfg.TursoURL
	if cfg.TursoToken != "" {
		dbURL = fmt.Sprintf("%s?authToken=%s", dbURL, cfg.TursoToken)
	}
	db, err := sql.Open("libsql", dbURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	tenant := *email
	if tenant == "" {
		tenant = os.Getenv("DEFAULT_USER_EMAIL")
	}
	if tenant == "" {
		tenant = "jjsong@whatap.io"
	}

	// Why: an empty --since must still match every row, so it degrades to a floor that
	// every timestamp clears rather than to a NULL comparison that matches nothing.
	floor := *since
	if floor == "" {
		floor = "0000-01-01"
	}

	fmt.Printf("=== extraction quality: %s (created_at >= %s) ===\n\n", tenant, floor)
	reportOutcomeBySource(db, tenant, floor)
	reportOutcomeByOwner(db, tenant, floor)
	reportOutcomeByCategory(db, tenant, floor)
	reportDrops(db, tenant, floor)
	reportLearningHealth(db, tenant)
}

// outcomeCols is the shared projection: a cancelled task is the user's own label for an
// extraction error, a completed one for a correct extraction.
const outcomeCols = `COUNT(*) AS total,
	SUM(CASE WHEN done = 0 AND is_deleted = 1 THEN 1 ELSE 0 END) AS canceled,
	SUM(CASE WHEN done = 1 THEN 1 ELSE 0 END) AS done_n,
	SUM(CASE WHEN is_deleted = 0 AND done = 0 THEN 1 ELSE 0 END) AS active`

func printOutcomeRows(rows *sql.Rows, label string) {
	defer rows.Close()
	fmt.Printf("%-34s %7s %9s %9s %8s\n", label, "total", "cancel%", "done%", "active")
	for rows.Next() {
		var key string
		var total, canceled, doneN, active int
		if err := rows.Scan(&key, &total, &canceled, &doneN, &active); err != nil {
			fmt.Printf("  scan error: %v\n", err)
			return
		}
		if total == 0 {
			continue
		}
		fmt.Printf("%-34s %7d %8.1f%% %8.1f%% %8d\n",
			"  "+key, total, pct(canceled, total), pct(doneN, total), active)
	}
	fmt.Println()
}

func pct(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return 100 * float64(part) / float64(whole)
}

func reportOutcomeBySource(db *sql.DB, tenant, floor string) {
	rows, err := db.Query(`SELECT COALESCE(source,'(none)'), `+outcomeCols+`
		FROM messages WHERE user_email = ? AND IFNULL(task,'') <> '' AND created_at >= ?
		GROUP BY source ORDER BY total DESC`, tenant, floor)
	if err != nil {
		fmt.Printf("by source: %v\n\n", err)
		return
	}
	printOutcomeRows(rows, "by source")
}

// reportOutcomeByOwner splits on whether anyone owns the task. Why: this was the largest
// precision lever found on 2026-09-10 -- shared cancelled at 66.4% on WhatsApp and 31.8%
// on Slack against 38.6% and 18.5% for a named assignee, and completed at half the rate.
func reportOutcomeByOwner(db *sql.DB, tenant, floor string) {
	rows, err := db.Query(`SELECT COALESCE(source,'(none)') || ' / ' ||
			CASE WHEN assignee = 'shared' THEN 'shared' ELSE 'named' END, `+outcomeCols+`
		FROM messages WHERE user_email = ? AND IFNULL(task,'') <> '' AND created_at >= ?
		GROUP BY source, CASE WHEN assignee = 'shared' THEN 'shared' ELSE 'named' END
		ORDER BY total DESC`, tenant, floor)
	if err != nil {
		fmt.Printf("by source/owner: %v\n\n", err)
		return
	}
	printOutcomeRows(rows, "by source / owner")
}

func reportOutcomeByCategory(db *sql.DB, tenant, floor string) {
	rows, err := db.Query(`SELECT COALESCE(NULLIF(category,''),'(empty)'), `+outcomeCols+`
		FROM messages WHERE user_email = ? AND IFNULL(task,'') <> '' AND created_at >= ?
		GROUP BY category ORDER BY total DESC`, tenant, floor)
	if err != nil {
		fmt.Printf("by category: %v\n\n", err)
		return
	}
	printOutcomeRows(rows, "by category")
}

// reportDrops is the other half of precision: what never became a task. Without it a
// tightened filter or a widened state=none looks identical to a quiet inbox.
func reportDrops(db *sql.DB, tenant, floor string) {
	rows, err := db.Query(`SELECT stage, verdict, COALESCE(NULLIF(source,''),'(none)'), COUNT(*)
		FROM extraction_decisions WHERE user_email = ? AND created_at >= ?
		GROUP BY stage, verdict, source ORDER BY COUNT(*) DESC`, tenant, floor)
	if err != nil {
		fmt.Printf("drops: %v (table may predate this build)\n\n", err)
		return
	}
	defer rows.Close()
	fmt.Printf("%-34s %7s\n", "dropped before becoming a task", "count")
	any := false
	for rows.Next() {
		var stage, verdict, source string
		var n int
		if err := rows.Scan(&stage, &verdict, &source, &n); err != nil {
			fmt.Printf("  scan error: %v\n", err)
			break
		}
		any = true
		fmt.Printf("%-34s %7d\n", fmt.Sprintf("  %s/%s (%s)", stage, verdict, source), n)
	}
	if !any {
		fmt.Println("  (none recorded -- no traffic in window, or telemetry not deployed)")
	}
	fmt.Println()
}

// reportLearningHealth surfaces whether the automated improve half is actually promoting
// anything. A wall of pending observations at evidence_count=1 means the keying is too
// exact to ever generalize, not that the user stopped correcting.
func reportLearningHealth(db *sql.DB, tenant string) {
	fmt.Printf("%-34s %7s %10s %9s\n", "learning loop", "obs", "evidence", "promoted")
	rows, err := db.Query(`SELECT kind, status, COUNT(*), COALESCE(SUM(evidence_count),0), MAX(evidence_count)
		FROM correction_observations WHERE user_email = ? GROUP BY kind, status ORDER BY COUNT(*) DESC`, tenant)
	if err != nil {
		fmt.Printf("  observations: %v\n", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var kind, status string
			var n, evidence, maxEvidence int
			if err := rows.Scan(&kind, &status, &n, &evidence, &maxEvidence); err != nil {
				fmt.Printf("  scan error: %v\n", err)
				break
			}
			fmt.Printf("%-34s %7d %10d %9s  (max evidence on one pattern: %d)\n",
				"  "+kind+" / "+status, n, evidence, status, maxEvidence)
		}
	}

	exRows, err := db.Query(`SELECT origin, COALESCE(NULLIF(source,''),'(none)'), COUNT(*)
		FROM learned_examples WHERE user_email = ? GROUP BY origin, source ORDER BY COUNT(*) DESC`, tenant)
	if err != nil {
		fmt.Printf("  learned examples: %v\n", err)
		return
	}
	defer exRows.Close()
	fmt.Println()
	fmt.Printf("%-34s %7s\n", "learned few-shots in use", "count")
	for exRows.Next() {
		var origin, source string
		var n int
		if err := exRows.Scan(&origin, &source, &n); err != nil {
			fmt.Printf("  scan error: %v\n", err)
			break
		}
		fmt.Printf("%-34s %7d\n", fmt.Sprintf("  %s (%s)", origin, source), n)
	}
}
