// Command rehearse-v15 replays the v13->v15 migration against a local copy of the
// production Turso database (schema + migration-critical data) before deploying.
// Prod connection is read-only by construction: only SELECT/PRAGMA run against it.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	"message-consolidator/config"
	"message-consolidator/store"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: rehearse-v15 <local-db-path>")
	}
	localPath := os.Args[1]
	ctx := context.Background()

	cfg := config.LoadConfig()
	if !strings.HasPrefix(cfg.TursoURL, "libsql://") {
		log.Fatal("TURSO_DATABASE_URL is not a libsql:// URL; refusing to rehearse against non-prod")
	}
	prod, err := sql.Open("libsql", cfg.TursoURL+"?authToken="+cfg.TursoToken)
	must(err, "open prod")
	defer prod.Close()

	_ = os.Remove(localPath)
	localDSN := "file:" + localPath + "?_pragma=busy_timeout(10000)"
	local, err := sql.Open("sqlite", localDSN)
	must(err, "open local")
	local.SetMaxOpenConns(1)

	ftsTables := replicateSchema(ctx, prod, local)
	copyTable(ctx, prod, local, "messages")
	copyTable(ctx, prod, local, "app_settings")
	// Why: external-content FTS starts empty in the replica; migration UPDATEs fire
	// sync triggers whose 'delete' commands corrupt an unindexed FTS (SQLITE_CORRUPT_VTAB).
	for _, fts := range ftsTables {
		_, err := local.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s(%s) VALUES('rebuild')", fts, fts))
		must(err, "rebuild "+fts)
		fmt.Printf("fts: %s rebuilt\n", fts)
	}

	preVer := scalar(local, `SELECT value FROM app_settings WHERE key='schema_version'`)
	preCounts := lifecycleCounts(local)
	fmt.Printf("PRE : schema_version=%s lifecycle=%v\n", preVer, preCounts)
	must(local.Close(), "close local")

	// Run the app's real init path: DDL replay + v14/v15 migrations + view rebuild.
	must(store.InitDB(ctx, &config.Config{TursoURL: localDSN}), "InitDB migration")
	db := store.GetDB()

	postVer := scalar(db, `SELECT value FROM app_settings WHERE key='schema_version'`)
	postCounts := lifecycleCounts(db)
	fmt.Printf("POST: schema_version=%s lifecycle=%v\n", postVer, postCounts)

	failures := 0
	check := func(name string, ok bool, detail string) {
		status := "PASS"
		if !ok {
			status = "FAIL"
			failures++
		}
		fmt.Printf("[%s] %s %s\n", status, name, detail)
	}

	check("version", postVer == "15", fmt.Sprintf("(%s -> %s)", preVer, postVer))
	check("lifecycle-counts-unchanged", fmt.Sprint(preCounts) == fmt.Sprint(postCounts), "")
	check("excluded_at-all-null", scalar(db, `SELECT COUNT(*) FROM messages WHERE excluded_at IS NOT NULL`) == "0", "")

	// Verification #3-2: sample row flips to lifecycle='excluded' and back.
	var sampleID string
	_ = db.QueryRow(`SELECT id FROM messages WHERE lifecycle='active' LIMIT 1`).Scan(&sampleID)
	_, err = db.Exec(`UPDATE messages SET excluded_at=CURRENT_TIMESTAMP WHERE id=?`, sampleID)
	must(err, "sample exclude")
	check("excluded-branch", scalar(db, `SELECT lifecycle FROM messages WHERE id=`+sampleID) == "excluded", "id="+sampleID)
	_, err = db.Exec(`UPDATE messages SET excluded_at=NULL WHERE id=?`, sampleID)
	must(err, "sample revert")
	check("excluded-revert", scalar(db, `SELECT lifecycle FROM messages WHERE id=`+sampleID) == "active", "")

	// Verification #3-3: v_messages exposes excluded_at; v14 sessions table exists.
	var one string
	err = db.QueryRow(`SELECT COUNT(excluded_at) FROM v_messages LIMIT 1`).Scan(&one)
	check("v_messages-excluded_at", err == nil, fmt.Sprintf("err=%v", err))
	check("sessions-table", scalar(db, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sessions'`) == "1", "")

	if failures > 0 {
		log.Fatalf("REHEARSAL FAILED: %d check(s)", failures)
	}
	fmt.Println("REHEARSAL OK")
}

// replicateSchema copies prod CREATE statements and returns the FTS virtual table
// names. FTS shadow tables are skipped (CREATE VIRTUAL TABLE recreates them), as are
// views/triggers (InitDB's DDL replay and rebuildViews recreate both).
func replicateSchema(ctx context.Context, prod, local *sql.DB) []string {
	rows, err := prod.QueryContext(ctx, `
		SELECT type, name, sql FROM sqlite_master
		WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'libsql_%'
		ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 WHEN 'trigger' THEN 2 ELSE 3 END`)
	must(err, "read sqlite_master")
	defer rows.Close()
	n := 0
	var ftsTables []string
	for rows.Next() {
		var typ, name, ddl string
		must(rows.Scan(&typ, &name, &ddl), "scan master")
		isVirtual := strings.HasPrefix(strings.ToUpper(ddl), "CREATE VIRTUAL TABLE")
		if strings.Contains(name, "_fts") && !isVirtual {
			continue
		}
		if typ == "view" || typ == "trigger" {
			continue
		}
		if _, err := local.ExecContext(ctx, ddl); err != nil {
			log.Fatalf("replicate %s %s: %v", typ, name, err)
		}
		if isVirtual {
			ftsTables = append(ftsTables, name)
		}
		n++
	}
	must(rows.Err(), "iterate master")
	fmt.Printf("schema: %d objects replicated (fts: %v)\n", n, ftsTables)
	return ftsTables
}

// copyTable streams all rows, excluding generated columns from the INSERT.
func copyTable(ctx context.Context, prod, local *sql.DB, table string) {
	cols := insertableColumns(local, table)
	colList := strings.Join(cols, ", ")
	placeholders := strings.TrimRight(strings.Repeat("?,", len(cols)), ",")

	rows, err := prod.QueryContext(ctx, "SELECT "+colList+" FROM "+table) //nolint:gosec // table names are compile-time constants
	must(err, "select "+table)
	defer rows.Close()

	tx, err := local.Begin()
	must(err, "begin")
	stmt, err := tx.Prepare("INSERT INTO " + table + " (" + colList + ") VALUES (" + placeholders + ")") //nolint:gosec
	must(err, "prepare insert")

	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	n := 0
	for rows.Next() {
		must(rows.Scan(ptrs...), "scan "+table)
		if _, err := stmt.Exec(vals...); err != nil {
			log.Fatalf("insert %s row %d: %v", table, n, err)
		}
		n++
	}
	must(rows.Err(), "iterate "+table)
	must(tx.Commit(), "commit "+table)
	fmt.Printf("data: %s %d rows\n", table, n)
}

func insertableColumns(db *sql.DB, table string) []string {
	rows, err := db.Query("PRAGMA table_xinfo(" + table + ")") //nolint:gosec
	must(err, "table_xinfo "+table)
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk, hidden int
		var dflt any
		must(rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk, &hidden), "scan xinfo")
		// hidden: 2/3 = generated columns — excluded from INSERT.
		if hidden == 0 {
			cols = append(cols, name)
		}
	}
	return cols
}

func lifecycleCounts(q interface {
	Query(string, ...any) (*sql.Rows, error)
}) map[string]int {
	out := map[string]int{}
	rows, err := q.Query(`SELECT lifecycle, COUNT(*) FROM messages GROUP BY lifecycle`)
	must(err, "lifecycle counts")
	defer rows.Close()
	for rows.Next() {
		var k string
		var v int
		must(rows.Scan(&k, &v), "scan counts")
		out[k] = v
	}
	return out
}

func scalar(q interface {
	QueryRow(string, ...any) *sql.Row
}, sqlText string) string {
	var v string
	if err := q.QueryRow(sqlText).Scan(&v); err != nil {
		return "ERR:" + err.Error()
	}
	return v
}

func must(err error, ctxMsg string) {
	if err != nil {
		log.Fatalf("%s: %v", ctxMsg, err)
	}
}
