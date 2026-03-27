package main

import (
	"fmt"
	"log"
	"os"

	"message-consolidator/config"
	"github.com/joho/godotenv"
)

func main() {
	// Root .env Load
	if err := godotenv.Load(); err != nil {
		// Suppress error if .env is missing, as system env might be used
	}

	if len(os.Args) < 2 {
		fmt.Println("Usage: mc-util <command> [args]")
		fmt.Println("Commands:")
		fmt.Println("  db-diag       : Database diagnostics (total counts, samples)")
		fmt.Println("  db-inspect    : Inspect specific message IDs")
		fmt.Println("  wa-pair       : WhatsApp CLI pairing tool")
		fmt.Println("  release-notes : Generate synchronized release notes")
		os.Exit(1)
	}

	cmd := os.Args[1]
	cfg := config.LoadConfig()

	switch cmd {
	case "db-diag":
		runDBDiag(cfg)
	case "db-inspect":
		runDBInspect(cfg)
	case "wa-pair":
		runWAPair(cfg)
	case "release-notes":
		runReleaseNotes(cfg)
	default:
		log.Fatalf("Unknown command: %s", cmd)
	}
}
