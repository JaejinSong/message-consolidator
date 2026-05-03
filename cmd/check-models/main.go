package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"google.golang.org/genai"
)

func main() {
	_ = godotenv.Load(".env")
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY not found in .env")
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Available Models:")
	for m, err := range client.Models.All(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("- %s (DisplayName: %s)\n", m.Name, m.DisplayName)
	}
}
