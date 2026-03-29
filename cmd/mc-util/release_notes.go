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

	// [FIX] Use more robust version parsing to find the REAL latest version
	latestVersion := "2.3.14" // Known baseline
	versionRegex := regexp.MustCompile(`v(\d+\.\d+\.\d+)`)

	for _, t := range targets {
		content, err := ioutil.ReadFile(t.Path)
		if err == nil {
			lines := strings.Split(string(content), "\n")
			// Only check first few lines to find the actual current version
			for i := 0; i < len(lines) && i < 10; i++ {
				matches := versionRegex.FindStringSubmatch(lines[i])
				if len(matches) > 1 {
					v := matches[1]
					// Simple semantic version comparison
					if compareVersions(v, latestVersion) > 0 {
						// Optional: blacklist "erroneous" versions if needed
						if !strings.HasPrefix(v, "2.5") && !strings.HasPrefix(v, "3.") {
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

	// Pre-check: If latestVersion is already v2.4.0 (our current goal), we might want to skip or bump to 2.4.1
	// For this normalization task, we strictly target 2.4.0
	targetVersion := "2.4.1" // Default next
	if latestVersion == "2.3.14" {
		targetVersion = "2.4.0"
	} else {
		targetVersion = incrementPatch(latestVersion)
	}

	log.Printf("Detected latest version: v%s. Targeting: v%s", latestVersion, targetVersion)

	promptPath := "ai/prompts/release_notes_combined.prompt"
	sysPromptRaw, _ := ioutil.ReadFile(promptPath)

	history := ""
	for _, t := range targets {
		content, _ := ioutil.ReadFile(t.Path)
		strContent := string(content)
		// Only send the last true history to AI to prevent inheriting errors
		if len(strContent) > 2000 {
			strContent = strContent[:2000]
		}
		history += fmt.Sprintf("\n--- %s History ---\n%s\n", t.Name, strContent)
	}

	sysPrompt := strings.ToValidUTF8(string(sysPromptRaw), "?")
	sysPrompt = strings.Replace(sysPrompt, "{LAST_RELEASE_NOTES}", strings.ToValidUTF8(history, "?"), 1)
	sysPrompt = strings.Replace(sysPrompt, "{NEW_COMMITS}", commitContext, 1)
	sysPrompt = strings.Replace(sysPrompt, "{DATE}", time.Now().UTC().Format("2006-01-02 15:04 UTC"), 1)
	sysPrompt = strings.Replace(sysPrompt, "{VERSION}", targetVersion, 1)

	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(sysPrompt)},
	}

	log.Printf("Generating synchronized release notes for v%s...", targetVersion)
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
			// [REFINEMENT] Defensive check: if file already has the target version at the top, DO NOT prepend it again
			currentContent, _ := ioutil.ReadFile(t.Path)
			if strings.Contains(string(currentContent), "v"+targetVersion) {
				log.Printf("Skip %s: version v%s already exists at top.", t.Name, targetVersion)
				continue
			}
			mcUpdateFile(t.Path, content)
			log.Printf("Updated %s", t.Name)
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
