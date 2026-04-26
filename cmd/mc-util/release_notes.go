package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"message-consolidator/ai"
	"message-consolidator/config"
	"message-consolidator/logger"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type ReleaseNotesJSON struct {
	TechEN string `json:"tech_en"`
	TechKO string `json:"tech_ko"`
	UserEN string `json:"user_en"`
	UserKO string `json:"user_ko"`
}

type TargetFile struct {
	Name string
	Path string
}

func runReleaseNotes(cfg *config.Config) {
	latestVer := getLatestVersion()
	lastTime := getLatestTimeFromFile("RELEASE_NOTES_USER_KO.md")
	commits := getCommitsSinceTime(lastTime)
	targetVersion := incrementPatch(strings.TrimPrefix(latestVer, "v"))

	logger.Infof("[RELEASE] Baseline: %s (%s), Next: v%s, New Commits: %d bytes", latestVer, lastTime, targetVersion, len(commits))

	notes := fetchReleaseNotes(cfg, commits, targetVersion)
	dispatchReleaseNotes(notes, targetVersion)
}

func getLatestVersion() string {
	tag := getLatestTag()
	fileVer := getLatestFromFile("RELEASE_NOTES_USER_KO.md")
	if compareVersions(strings.TrimPrefix(tag, "v"), strings.TrimPrefix(fileVer, "v")) >= 0 {
		return tag
	}
	return fileVer
}

func getLatestFromFile(path string) string {
	content, _ := os.ReadFile(path)
	re := regexp.MustCompile(`v(\d+\.\d+\.\d+)`)
	matches := re.FindAllStringSubmatch(string(content), -1)
	max := "2.4.0"
	for _, m := range matches {
		if len(m) > 1 && compareVersions(m[1], max) > 0 {
			max = m[1]
		}
	}
	return "v" + max
}

func getLatestTimeFromFile(path string) string {
	content, _ := os.ReadFile(path)
	re := regexp.MustCompile(`\((\d{4}-\d{2}-\d{2} \d{2}:\d{2}) UTC\)`)
	match := re.FindStringSubmatch(string(content))
	if len(match) > 1 {
		return match[1]
	}
	return time.Now().Add(-24 * time.Hour).Format("2006-01-02 15:04")
}

func getLatestTag() string {
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
	out, err := cmd.Output()
	if err != nil {
		return "v2.2.3" // Fallback to last known good tag
	}
	return strings.TrimSpace(string(out))
}

func getCommitsSinceTime(lastTime string) string {
	// Format: git log --since="2026-04-03 02:26" --pretty=format:%s
	cmd := exec.Command("git", "log", "--since="+lastTime, "--pretty=format:%s", "--no-merges")
	out, _ := cmd.CombinedOutput()
	commits := strings.ToValidUTF8(string(out), "?")

	if len(commits) > 2000 {
		return commits[:2000] + "\n... (truncated)"
	}
	return commits
}

func fetchReleaseNotes(cfg *config.Config, commits, version string) ReleaseNotesJSON {
	ctx := context.Background()
	client, _ := genai.NewClient(ctx, option.WithAPIKey(cfg.GeminiAPIKey))
	defer client.Close()

	model := client.GenerativeModel("gemini-3-flash-preview")
	model.ResponseMIMEType = "application/json"

	prompt := loadPrompt(commits, version)
	logger.Infof("[AI] Payload size: %d bytes", len(prompt))

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		log.Fatal(err)
	}
	return parseAIResponse(resp)
}

func loadPrompt(commits, version string) string {
	parsed := ai.LoadPrompt(ai.PromptReleaseNotesCombined)
	data := ai.ExtractionContext{
		Version:        version,
		MessagePayload: commits,
	}
	rendered, err := parsed.Render(data)
	if err != nil {
		logger.Errorf("[RELEASE] Failed to render prompt: %v", err)
		return parsed.Body
	}
	return rendered
}

func parseAIResponse(resp *genai.GenerateContentResponse) ReleaseNotesJSON {
	var result ReleaseNotesJSON
	if len(resp.Candidates) == 0 {
		return result
	}
	part := resp.Candidates[0].Content.Parts[0]
	if txt, ok := part.(genai.Text); ok {
		_ = json.Unmarshal([]byte(txt), &result)
	}
	return result
}

func dispatchReleaseNotes(notes ReleaseNotesJSON, version string) {
	dateStr := time.Now().UTC().Format("2006-01-02 15:04 UTC")
	mapping := map[string]struct {
		path   string
		header string
		body   string
	}{
		"TECH_EN": {"RELEASE_NOTES_TECH_EN.md", "# Release Notes (Tech) - v%s (%s)", notes.TechEN},
		"TECH_KO": {"RELEASE_NOTES_TECH_KO.md", "# 업데이트 소식 (기술) - v%s (%s)", notes.TechKO},
		"USER_EN": {"RELEASE_NOTES_USER_EN.md", "# Release Notes - v%s (%s)", notes.UserEN},
		"USER_KO": {"RELEASE_NOTES_USER_KO.md", "# 업데이트 소식 - v%s (%s)", notes.UserKO},
	}

	for _, m := range mapping {
		full := fmt.Sprintf(m.header, version, dateStr) + "\n\n" + strings.TrimSpace(m.body)
		writeWithBackup(m.path, full, version)
	}
}

func writeWithBackup(path, content, version string) {
	current, _ := os.ReadFile(path)
	if strings.Contains(string(current), "v"+version) {
		logger.Infof("[SKIP] %s already updated", path)
		return
	}

	_ = os.WriteFile(path+".bak", current, 0644)
	newContent := content + "\n\n---\n\n" + string(current)
	_ = os.WriteFile(path, []byte(newContent), 0644)
	logger.Infof("[DONE] Updated %s", path)
}

// Helper to compare versions (a > b -> 1, a < b -> -1, a == b -> 0)
func compareVersions(a, b string) int {
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	for i := 0; i < 3 && i < len(ap) && i < len(bp); i++ {
		var av, bv int
		_, _ = fmt.Sscanf(ap[i], "%d", &av)
		_, _ = fmt.Sscanf(bp[i], "%d", &bv)
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
	_, _ = fmt.Sscanf(parts[0], "%d", &major)
	_, _ = fmt.Sscanf(parts[1], "%d", &minor)
	_, _ = fmt.Sscanf(parts[2], "%d", &patch)
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
}

// End of release_notes.go
