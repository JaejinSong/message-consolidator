package main

import (
	"fmt"
	"log"
	"os"

	"message-consolidator/config"
	"message-consolidator/logger"
	"github.com/joho/godotenv"
)

func main() {
	// Why: Ignores missing .env errors — env vars may come from host/Docker/Cloud Run instead.
	_ = godotenv.Load()

	logger.InitLogging()

	if len(os.Args) < 2 {
		fmt.Println("Usage: mc-util <command> [args]")
		fmt.Println("Commands:")
		fmt.Println("  db-diag       : Database diagnostics (total counts, samples)")
		fmt.Println("  wa-pair       : WhatsApp CLI pairing tool")
		fmt.Println("  release-notes : Generate synchronized release notes")
		fmt.Println("  dedup-tasks   : Remove duplicate [Update:] sections from task fields")
		os.Exit(1)
	}

	cmd := os.Args[1]
	cfg := config.LoadConfig()

	switch cmd {
	case "db-diag":
		runDBDiag(cfg)
	case "wa-pair":
		runWAPair(cfg)
	case "release-notes":
		runReleaseNotes(cfg)
	case "dedup-tasks":
		runDedupTasks(cfg)
	default:
		log.Fatalf("Unknown command: %s", cmd)
	}
}
