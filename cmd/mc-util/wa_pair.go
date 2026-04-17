package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"message-consolidator/channels"
	"message-consolidator/config"
	"message-consolidator/store"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

func runWAPair(cfg *config.Config) {
	fmt.Println("=== WhatsApp CLI Pairing Tool ===")
	fmt.Println("Connecting to Turso DB...")

	if cfg.TursoURL == "" {
		fmt.Println("Error: TURSO_DATABASE_URL is not set in .env")
		os.Exit(1)
	}

	if err := store.InitDB(context.Background(), cfg); err != nil {
		fmt.Printf("Error initializing DB: %v\n", err)
		os.Exit(1)
	}

	email := "jjsong@whatap.io" //Why: Hardcodes the primary developer's email as the default registration target for manual CLI pairing sessions.
	fmt.Printf("Initializing WhatsApp for %s...\n", email)

	//Why: Overrides the user metadata fetcher to bypass existing JID checks during a fresh CLI pairing attempt.
	channels.DefaultWAManager.FetchUserWAJID = func(email string) (string, error) {
		return "", nil 
	}

	channels.DefaultWAManager.InitWhatsApp(email, cfg)
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("Generating QR Code...")
	encodedPNG, err := channels.DefaultWAManager.GetQR(ctx, email)
	if err != nil {
		fmt.Printf("Error getting QR: %v\n", err)
		os.Exit(1)
	}

	if encodedPNG == "CONNECTED" {
		fmt.Println("Already connected!")
		os.Exit(0)
	}

	//Why: Converts the base64-encoded QR code string back into raw PNG bytes for physical storage and user viewing.
	pngData, err := base64.StdEncoding.DecodeString(encodedPNG)
	if err != nil {
		fmt.Printf("Error decoding base64: %v\n", err)
		os.Exit(1)
	}

	//Why: Writes the QR code to a local PNG file to facilitate scanning from a standard image viewer during the CLI pairing flow.
	qrFile := "whatsapp_qr.png"
	err = os.WriteFile(qrFile, pngData, 0644)
	if err != nil {
		fmt.Printf("Error saving QR to file: %v\n", err)
	} else {
		fmt.Printf("\n[SUCCESS] QR Code saved to %s\n", qrFile)
		fmt.Println("Please open the image and scan it with your WhatsApp mobile app.")
	}

	fmt.Println("\nWaiting for connection success... (Ctrl+C to stop)")
	
	//Why: Blocks the main thread to allow the WhatsApp background workers to process the pairing success event after the user scans the QR code.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\nExiting...")
}
