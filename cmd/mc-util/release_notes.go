package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"message-consolidator/config"
	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type TargetFile struct {
	Name string
	Path string
}

func runReleaseNotes(cfg *config.Config) {
	apiKey := cfg.GeminiAPIKey
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

	targets := []TargetFile{
		{"RELEASE_NOTES.md", "RELEASE_NOTES.md"},
		{"RELEASE_NOTES_KR.md", "RELEASE_NOTES_KR.md"},
		{"RELEASE_NOTES_USER.md", "RELEASE_NOTES_USER.md"},
	}

	lastVersion := "2.3.4"
	versionRegex := regexp.MustCompile(`v(\d+\.\d+\.\d+)`)

	for _, t := range targets {
		content, err := ioutil.ReadFile(t.Path)
		if err == nil {
			matches := versionRegex.FindStringSubmatch(string(content))
			if len(matches) > 1 {
				if matches[1] > lastVersion {
					lastVersion = matches[1]
				}
			}
		}
	}

	cmd := exec.Command("git", "log", "-n", "30", "--pretty=format:%h: %s")
	out, _ := cmd.CombinedOutput()
	commitContext := strings.ToValidUTF8(string(out), "?")

	promptPath := "ai/prompts/release_notes_combined.prompt"
	sysPromptRaw, _ := ioutil.ReadFile(promptPath)

	history := ""
	for _, t := range targets {
		content, _ := ioutil.ReadFile(t.Path)
		strContent := string(content)
		if len(strContent) > 2000 {
			strContent = strContent[:2000]
		}
		history += fmt.Sprintf("\n--- %s History ---\n%s\n", t.Name, strContent)
	}

	nextVersion := incrementPatch(lastVersion)
	sysPrompt := strings.ToValidUTF8(string(sysPromptRaw), "?")
	sysPrompt = strings.Replace(sysPrompt, "{LAST_RELEASE_NOTES}", strings.ToValidUTF8(history, "?"), 1)
	sysPrompt = strings.Replace(sysPrompt, "{NEW_COMMITS}", commitContext, 1)
	sysPrompt = strings.Replace(sysPrompt, "{DATE}", time.Now().Format("2006-01-02 15:04"), 1)
	sysPrompt = strings.Replace(sysPrompt, "{VERSION}", nextVersion, 1)

	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(sysPrompt)},
	}

	log.Printf("Generating synchronized release notes for v%s...", nextVersion)
	resp, err := model.GenerateContent(ctx, genai.Text("Generate the combined release notes block now. Strictly follow the [FILENAME] header format."))
	if err != nil {
		log.Fatal(fmt.Errorf("AI generation failed: %w", err))
	}

	var fullResponse string
	for _, candidate := range resp.Candidates {
		for _, part := range candidate.Content.Parts {
			if t, ok := part.(genai.Text); ok {
				fullResponse += string(t)
			}
		}
	}

	sections := mcSplitSections(fullResponse)
	for _, t := range targets {
		if content, ok := sections[t.Name]; ok {
			mcUpdateFile(t.Path, content)
			log.Printf("Updated %s", t.Name)
		}
	}
}

func incrementPatch(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) != 3 { return v }
	var major, minor, patch int
	fmt.Sscanf(parts[0], "%d", &major)
	fmt.Sscanf(parts[1], "%d", &minor)
	fmt.Sscanf(parts[2], "%d", &patch)
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
}

func mcSplitSections(resp string) map[string]string {
	sections := make(map[string]string)
	filenames := []string{"RELEASE_NOTES.md", "RELEASE_NOTES_KR.md", "RELEASE_NOTES_USER.md"}
	for _, fname := range filenames {
		tag := "[" + fname + "]"
		start := strings.Index(resp, tag)
		if start == -1 { continue }
		start += len(tag)
		end := len(resp)
		for _, nextFname := range filenames {
			if fname == nextFname { continue }
			nextTag := "[" + nextFname + "]"
			nStart := strings.Index(resp[start:], nextTag)
			if nStart != -1 && nStart+start < end {
				end = nStart + start
			}
		}
		content := strings.TrimSpace(resp[start:end])
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		sections[fname] = strings.TrimSpace(content)
	}
	return sections
}

func mcUpdateFile(path, newContent string) {
	oldContent, _ := ioutil.ReadFile(path)
	finalContent := newContent + "\n\n---\n\n" + string(oldContent)
	_ = ioutil.WriteFile(path, []byte(finalContent), 0644)
}
