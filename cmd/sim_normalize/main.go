package main

import (
	"context"
	"fmt"
	"os"

	"message-consolidator/config"
	"message-consolidator/store"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

func main() {
	cfg := config.LoadConfig()
	if cfg.TursoURL == "" {
		fmt.Println("ERR: TURSO_DATABASE_URL not set")
		os.Exit(1)
	}
	ctx := context.Background()
	if err := store.InitDB(ctx, cfg); err != nil {
		fmt.Printf("ERR InitDB: %v\n", err)
		os.Exit(1)
	}

	email := "jjsong@whatap.io"
	cases := []string{"shared", "sunpho", "Hady", "Handi", "__current_user__", "me", "Jaejin Song (JJ)", "jjsong@whatap.io", "송재진"}

	for _, c := range cases {
		out := store.NormalizeName(ctx, email, c)
		fmt.Printf("NormalizeName(%-25q) = %q\n", c, out)
	}

	fmt.Println("\n--- ResolveAlias direct ---")
	for _, c := range []string{"shared", "sunpho"} {
		id, err := store.ResolveAlias(ctx, store.ContactTypeName, c)
		fmt.Printf("ResolveAlias(name, %q) → id=%d, err=%v\n", c, id, err)
	}
}
