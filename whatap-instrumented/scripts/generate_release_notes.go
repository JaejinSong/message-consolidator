package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/whatap/go-api/instrumentation/fmt/whatapfmt"
	"github.com/whatap/go-api/logsink"
	"github.com/whatap/go-api/trace"
	"google.golang.org/api/option"
)

func main() {
	trace.Init(nil)
	defer trace.Shutdown()
	log.SetOutput(logsink.GetTraceLogWriter(os.Stderr))
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY is not set")
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-3-flash-preview")

	// 1. Get previous release notes (Full Context for Redundancy Check)
	relFile := "RELEASE_NOTES_USER.md"
	content, err := ioutil.ReadFile(relFile)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to read %s: %w", relFile, err))
	}

	// Extract the latest version from the file to suggest the next one
	versionRegex := regexp.MustCompile(`# 업데이트 소식 \(사용자용\) - v(\d+\.\d+\.\d+)`)
	matches := versionRegex.FindStringSubmatch(string(content))
	lastVersion := "2.1.3"
	if len(matches) > 1 {
		lastVersion = matches[1]
	}

	// 2. Get new commits since the last version tag in git log
	cmd := exec.Command("git", "log", "-n", "30", "--pretty=format:%h: %s")
	out, err := cmd.Output()
	if err != nil {
		log.Fatal(fmt.Errorf("failed to get git log: %w", err))
	}

	lines := strings.Split(string(out), "\n")
	newCommits := []string{}
	for _, line := range lines {
		if strings.Contains(line, "v"+lastVersion) || strings.Contains(line, "("+lastVersion+")") {
			break
		}
		newCommits = append(newCommits, line)
	}

	commitContext := strings.Join(newCommits, "\n")
	if len(newCommits) == 0 {
		log.Printf("Warning: No new commits found since v%s. Showing last 5 commits for context anyway.", lastVersion)
		commitContext = strings.Join(lines[:5], "\n")
	}

	// 3. Load System Prompt
	promptPath := "ai/prompts/release_notes_system.prompt"
	sysPromptRaw, err := ioutil.ReadFile(promptPath)
	if err != nil {
		log.Fatal(fmt.Errorf("failed to read prompt file %s: %w", promptPath, err))
	}

	// 4. Interpolate Prompt
	sysPrompt := string(sysPromptRaw)
	sysPrompt = strings.Replace(sysPrompt, "{LAST_RELEASE_NOTES}", string(content), 1) // Full file
	sysPrompt = strings.Replace(sysPrompt, "{NEW_COMMITS}", commitContext, 1)
	sysPrompt = strings.Replace(sysPrompt, "{DATE}", time.Now().Format("2006-01-02 15:04"), 1)

	nextVersion := incrementPatch(lastVersion)
	sysPrompt = strings.Replace(sysPrompt, "{VERSION}", nextVersion, 1)

	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(sysPrompt)},
	}

	log.Printf("Generating release notes for v%s...", nextVersion)
	resp, err := model.GenerateContent(ctx, genai.Text("Generate the new release notes block. Be extremely strict: if an improvement was already detailed in the provided history, SKIP it."))
	if err != nil {
		log.Fatal(fmt.Errorf("AI generation failed: %w", err))
	}

	whatapfmt.Println("\n--- GENERATED RELEASE NOTES (PRE-VERSION) ---")
	for _, candidate := range resp.Candidates {
		for _, part := range candidate.Content.Parts {
			if t, ok := part.(genai.Text); ok {
				whatapfmt.Println(string(t))
			}
		}
	}
	whatapfmt.Println("-------------------------------")
}

func incrementPatch(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return v
	}
	var patch int
	fmt.Sscanf(parts[2], "%d", &patch)
	return fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1)
}
