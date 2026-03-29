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
		{"RELEASE_NOTES_TECH_EN.md", "RELEASE_NOTES_TECH_EN.md"},
		{"RELEASE_NOTES_TECH_KO.md", "RELEASE_NOTES_TECH_KO.md"},
		{"RELEASE_NOTES_USER_EN.md", "RELEASE_NOTES_USER_EN.md"},
		{"RELEASE_NOTES_USER_KO.md", "RELEASE_NOTES_USER_KO.md"},
	}

	latestVersion := "2.3.14"
	versionRegex := regexp.MustCompile(`v(\d+\.\d+\.\d+)`)

	for _, t := range targets {
		content, err := ioutil.ReadFile(t.Path)
		if err == nil {
			lines := strings.Split(string(content), "\n")
			for i := 0; i < len(lines) && i < 15; i++ {
				matches := versionRegex.FindStringSubmatch(lines[i])
				if len(matches) > 1 {
					v := matches[1]
					if compareVersions(v, latestVersion) > 0 {
						// Strictly ignore erroneous v2.5.x for now to force correct baseline
						if !strings.HasPrefix(v, "2.5") {
							latestVersion = v
						}
					}
				}
			}
		}
	}

	cmd := exec.Command("git", "log", "-n", "30", "--pretty=format:%h: %s")
	out, _ := cmd.CombinedOutput()
	commitContext := strings.ToValidUTF8(string(out), "?")

	targetVersion := incrementPatch(latestVersion)
	// For this specific normalization task, if latest is 2.4.0, we want 2.4.1
	if latestVersion == "2.4.0" {
		targetVersion = "2.4.1"
	}

	log.Printf("Detected latest version: v%s. Targeting: v%s", latestVersion, targetVersion)

	promptPath := "ai/prompts/release_notes_combined.prompt"
	sysPromptRaw, _ := ioutil.ReadFile(promptPath)

	history := ""
	for _, t := range targets {
		content, _ := ioutil.ReadFile(t.Path)
		strContent := string(content)
		// [PRUNING] Only provide the first 1000 chars of history to prevent pollution
		if len(strContent) > 1000 {
			strContent = strContent[:1000]
		}
		history += fmt.Sprintf("\n--- %s Recent History ---\n%s\n", t.Name, strContent)
	}

	sysPrompt := strings.ToValidUTF8(string(sysPromptRaw), "?")
	sysPrompt = strings.Replace(sysPrompt, "{LAST_RELEASE_NOTES}", strings.ToValidUTF8(history, "?"), 1)
	sysPrompt = strings.Replace(sysPrompt, "{NEW_COMMITS}", commitContext, 1)
	sysPrompt = strings.Replace(sysPrompt, "{VERSION}", targetVersion, 1)

	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(sysPrompt)},
	}

	log.Printf("Generating synchronized release notes body for v%s...", targetVersion)
	resp, err := model.GenerateContent(ctx, genai.Text("Generate the bullet points now. No headers."))
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
	dateStr := time.Now().UTC().Format("2006-01-02 15:04 UTC")

	for _, t := range targets {
		if body, ok := sections[t.Name]; ok {
			// [FUNDAMENTAL] Forcefully construct the header in Go
			var header string
			switch t.Name {
			case "RELEASE_NOTES_TECH_EN.md":
				header = fmt.Sprintf("# Release Notes (Tech) - v%s (%s)", targetVersion, dateStr)
			case "RELEASE_NOTES_TECH_KO.md":
				header = fmt.Sprintf("# 업데이트 소식 (기술) - v%s (%s)", targetVersion, dateStr)
			case "RELEASE_NOTES_USER_EN.md":
				header = fmt.Sprintf("# Release Notes - v%s (%s)", targetVersion, dateStr)
			case "RELEASE_NOTES_USER_KO.md":
				header = fmt.Sprintf("# 업데이트 소식 - v%s (%s)", targetVersion, dateStr)
			}

			// Clean the body to remove any accidental AI headers
			cleanBody := strings.TrimSpace(body)
			if strings.HasPrefix(cleanBody, "#") {
				// Strip AI-generated headers if any
				lines := strings.Split(cleanBody, "\n")
				var filtered []string
				for _, l := range lines {
					if !strings.HasPrefix(strings.TrimSpace(l), "#") {
						filtered = append(filtered, l)
					}
				}
				cleanBody = strings.TrimSpace(strings.Join(filtered, "\n"))
			}

			fullBlock := header + "\n\n" + cleanBody

			// Defensive check: if file already has the target version at the top, skip
			currentContent, _ := ioutil.ReadFile(t.Path)
			if strings.Contains(string(currentContent), "v"+targetVersion) {
				log.Printf("Skip %s: version v%s already exists.", t.Name, targetVersion)
				continue
			}

			mcUpdateFile(t.Path, fullBlock)
			log.Printf("Updated %s with Go-controlled header", t.Name)
		}
	}
}

// Helper to compare versions (a > b -> 1, a < b -> -1, a == b -> 0)
func compareVersions(a, b string) int {
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	for i := 0; i < 3 && i < len(ap) && i < len(bp); i++ {
		var av, bv int
		fmt.Sscanf(ap[i], "%d", &av)
		fmt.Sscanf(bp[i], "%d", &bv)
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}

func incrementPatch(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return v
	}
	var major, minor, patch int
	fmt.Sscanf(parts[0], "%d", &major)
	fmt.Sscanf(parts[1], "%d", &minor)
	fmt.Sscanf(parts[2], "%d", &patch)
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
}

func mcSplitSections(resp string) map[string]string {
	sections := make(map[string]string)
	filenames := []string{"RELEASE_NOTES_TECH_EN.md", "RELEASE_NOTES_TECH_KO.md", "RELEASE_NOTES_USER_EN.md", "RELEASE_NOTES_USER_KO.md"}
	for _, fname := range filenames {
		tag := "[" + fname + "]"
		start := strings.Index(resp, tag)
		if start == -1 {
			continue
		}
		start += len(tag)
		end := len(resp)
		for _, nextFname := range filenames {
			if fname == nextFname {
				continue
			}
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
