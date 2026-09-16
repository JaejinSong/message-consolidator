//go:build chat_replay

package ai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"message-consolidator/ai/core"
	"message-consolidator/store"
)

// Why: chat_system v1.17.0 asks for one entry per message plus subtasks. The rule only
// matters if the live model obeys it, so replay the real payloads that produced several
// entries for one source_ts — the shape that UNIQUE(user_email, source_ts) silently
// truncated — and measure how many still split.
//
//	set -a; source .env; set +a; CHAT_REPLAY_DIR=<dir> \
//	  go test -tags=chat_replay ./ai/ -run TestChatExtractionReplay -v -timeout 30m
func TestChatExtractionReplay(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set — skipping chat extraction replay")
	}
	dir := os.Getenv("CHAT_REPLAY_DIR")
	if dir == "" {
		t.Fatal("CHAT_REPLAY_DIR not set — point it at a directory of *.txt message payloads")
	}
	cases, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil || len(cases) == 0 {
		t.Fatalf("no *.txt payloads in %s (err=%v)", dir, err)
	}
	sort.Strings(cases)

	c, err := NewAIClient(context.Background(), ProviderConfig{
		Provider:        "deepseek",
		DeepSeekAPIKey:  key,
		DeepSeekBaseURL: os.Getenv("DEEPSEEK_BASE_URL"),
	})
	if err != nil {
		t.Fatalf("NewAIClient(deepseek): %v", err)
	}
	model := os.Getenv("CHAT_REPLAY_MODEL")
	if model == "" {
		model = "deepseek-v4.1-flash"
	}

	analyzer := core.GetAnalyzer(store.SourceSlack)
	var split, folded, withSubtasks, suppressed int

	for _, path := range cases {
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		data := core.ExtractionContext{
			MessagePayload:    string(payload),
			CurrentTime:       time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
			Locale:            "Korean",
			ExistingTasksJSON: "[]",
			CurrentUser:       "송재진",
			CurrentUserEmail:  "jjsong@whatap.io",
			Source:            store.SourceSlack,
			ChatType:          "group",
		}
		req := LLMRequest{
			Model:       model,
			System:      analyzer.GetSystemInstruction(data),
			User:        analyzer.GetUserPrompt(data),
			Temperature: 0.0,
			MaxTokens:   DefaultMaxTokens,
			JSONMode:    true,
			Thinking:    ThinkOn,
		}
		resp, genErr := c.transport.Generate(context.Background(), req, 180*time.Second, 1)
		if genErr != nil {
			t.Errorf("[%s] generate failed: %v", filepath.Base(path), genErr)
			continue
		}

		if outDir := os.Getenv("CHAT_REPLAY_OUT"); outDir != "" {
			name := strings.TrimSuffix(filepath.Base(path), ".txt") + ".response.json"
			if werr := os.WriteFile(filepath.Join(outDir, name), []byte(resp.Text), 0o644); werr != nil {
				t.Fatalf("write response: %v", werr)
			}
		}

		var items []struct {
			State    string `json:"state"`
			Task     string `json:"task"`
			SourceTS string `json:"source_ts"`
			Subtasks []struct {
				Task string `json:"task"`
			} `json:"subtasks"`
		}
		if err := json.Unmarshal([]byte(core.SanitizeJSON(resp.Text)), &items); err != nil {
			t.Errorf("[%s] unparsable response: %v\n%s", filepath.Base(path), err, resp.Text)
			continue
		}

		perTS := map[string]int{}
		subtasksOnWinner, extracted := 0, 0
		for _, it := range items {
			if it.State != "new" || it.Task == "" {
				continue
			}
			extracted++
			perTS[it.SourceTS]++
			if len(it.Subtasks) > subtasksOnWinner {
				subtasksOnWinner = len(it.Subtasks)
			}
		}
		worst := 0
		for _, n := range perTS {
			if n > worst {
				worst = n
			}
		}
		switch {
		// Why: an all-`none` verdict is the broadcast-suppression rule, not a fold —
		// counting it as success once hid three real collapses behind "subtasks=0".
		case extracted == 0:
			suppressed++
			t.Logf("[%s] suppressed: no new task extracted (state=none)", filepath.Base(path))
		case worst > 1:
			split++
			t.Errorf("[%s] still splits one message into %d entries (subtasks on winner: %d)",
				filepath.Base(path), worst, subtasksOnWinner)
		default:
			folded++
			if subtasksOnWinner > 0 {
				withSubtasks++
			}
			t.Logf("[%s] folded to one entry, subtasks=%d", filepath.Base(path), subtasksOnWinner)
		}
	}

	t.Logf("replayed %d cases: folded=%d (with subtasks=%d) still-split=%d suppressed=%d",
		len(cases), folded, withSubtasks, split, suppressed)
}
