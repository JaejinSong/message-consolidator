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

func main() {
	fmt.Println("=== WhatsApp CLI Pairing Tool ===")
	fmt.Println("Connecting to Turso DB...")

	cfg := config.LoadConfig()
	if cfg.TursoURL == "" {
		fmt.Println("Error: TURSO_DATABASE_URL is not set in .env")
		os.Exit(1)
	}

	err := store.InitDB(cfg)
	if err != nil {
		fmt.Printf("Error initializing DB: %v\n", err)
		os.Exit(1)
	}

	email := "jjsong@whatap.io" // Default target email
	fmt.Printf("Initializing WhatsApp for %s...\n", email)

	// Custom fetcher to handle pairing context
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

	// Decode base64 PNG
	pngData, err := base64.StdEncoding.DecodeString(encodedPNG)
	if err != nil {
		fmt.Printf("Error decoding base64: %v\n", err)
		os.Exit(1)
	}

	// Save QR to file
	qrFile := "whatsapp_qr.png"
	err = os.WriteFile(qrFile, pngData, 0644)
	if err != nil {
		fmt.Printf("Error saving QR to file: %v\n", err)
	} else {
		fmt.Printf("\n[SUCCESS] QR Code saved to %s\n", qrFile)
		fmt.Println("Please open the image and scan it with your WhatsApp mobile app.")
	}

	fmt.Println("\nWaiting for connection success... (Ctrl+C to stop)")
	
	// Keep running to handle events (like Connected)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\nExiting...")
}
