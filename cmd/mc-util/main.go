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
	//Why: Loads local environment variables from the root .env file to support local development and manual diagnostic runs.
	if err := godotenv.Load(); err != nil {
		//Why: Ignores missing .env file errors because environment variables may already be set in the host system or via Docker/Cloud Run.
	}

	logger.InitLogging()

	if len(os.Args) < 2 {
		fmt.Println("Usage: mc-util <command> [args]")
		fmt.Println("Commands:")
		fmt.Println("  db-diag       : Database diagnostics (total counts, samples)")
		fmt.Println("  wa-pair       : WhatsApp CLI pairing tool")
		fmt.Println("  release-notes : Generate synchronized release notes")
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
	default:
		log.Fatalf("Unknown command: %s", cmd)
	}
}
