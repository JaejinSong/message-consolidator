package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

type TodoItem struct {
	Task         string `json:"task"`
	Requester    string `json:"requester"`
	Assignee     string `json:"assignee"`
	AssignedAt   string `json:"assigned_at"`
	SourceTS     string `json:"source_ts"`
	OriginalText string `json:"original_text"`
}

func main() {
	godotenv.Load("/home/jinro/.gemini/message-consolidator/.env")
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY not set")
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	modelName := "gemini-3-flash-preview"
	model := client.GenerativeModel(modelName)
	model.ResponseMIMEType = "application/json"
	language := "Korean"
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(fmt.Sprintf(`Extract tasks as a JSON array: [{"task", "requester", "assignee", "assigned_at", "source_ts", "original_text"}]
1. "task": Concise task description in %s.
2. "requester": The exact name of the person requesting the task.
3. "assignee": The exact name of the person responsible for the task. Do NOT use generic pronouns like '나', '저', 'me', 'you'. Resolve pronouns to actual names based on context (e.g., sender/recipient). If someone is mentioned with '@', prioritize that name. If strictly unknown, leave it empty.
4. "original_text": The literal original text of the message (single-line, no modification).
5. "source_ts": Find via [TS:timestamp].`, language))},
	}

	// Simulated WhatsApp message based on user screenshot
	conversationText := `Analyze this whatsapp chat:
[TS:1742464821] [11:00] ~ Laurence Liong (梁威浩): @58102435057696 @1949056666256901 we hv another Cambodia bank asking for intro n demo session on Monday 23-Mar 10am-11am or 3-4pm (Msia time), r u guys available with this short notice?
`

	resp, err := model.GenerateContent(ctx, genai.Text(conversationText))
	if err != nil {
		log.Fatal(err)
	}

	var rawJSON string
	for _, part := range resp.Candidates[0].Content.Parts {
		if t, ok := part.(genai.Text); ok {
			rawJSON += string(t)
		}
	}

	fmt.Println("Raw JSON Output:")
	fmt.Println(rawJSON)

	var items []TodoItem
	if err := json.Unmarshal([]byte(rawJSON), &items); err != nil {
		log.Fatalf("Unmarshal failed: %v", err)
	}

	for i, item := range items {
		fmt.Printf("Item %d:\n", i+1)
		fmt.Printf("  Task: %s\n", item.Task)
		fmt.Printf("  Requester: %s\n", item.Requester)
		fmt.Printf("  Assignee: %s\n", item.Assignee)
		fmt.Printf("  SourceTS: %s\n", item.SourceTS)
	}
}
