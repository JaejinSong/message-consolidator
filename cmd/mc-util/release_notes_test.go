package main

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"message-consolidator/config"
)

type failingReleaseTransport struct{}

func (failingReleaseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	_ = os.WriteFile("request-reached", []byte("yes"), 0600)
	return nil, errors.New("offline generation failure")
}

func TestReleaseNotesFailurePreservesFiles(t *testing.T) {
	for _, key := range []string{"GOOGLE_API_KEY", "GEMINI_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_GENAI_USE_ENTERPRISE", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "GOOGLE_CLOUD_REGION"} {
		t.Setenv(key, "")
	}
	for _, scenario := range []string{"initialization", "generation"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"RELEASE_NOTES_TECH_EN.md", "RELEASE_NOTES_TECH_KO.md", "RELEASE_NOTES_USER_EN.md", "RELEASE_NOTES_USER_KO.md"} {
				for _, suffix := range []string{"", ".bak"} {
					if err := os.WriteFile(filepath.Join(dir, name+suffix), []byte("original "+name+suffix), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestReleaseNotesFailureHelper$")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "RELEASE_NOTES_TEST_SCENARIO="+scenario)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("failure must return without panic or process exit: %v\n%s", err, output)
			}
			if scenario == "generation" {
				if _, err := os.Stat(filepath.Join(dir, "request-reached")); err != nil {
					t.Errorf("fake transport was not reached: %v", err)
				}
			}
			for _, name := range []string{"RELEASE_NOTES_TECH_EN.md", "RELEASE_NOTES_TECH_KO.md", "RELEASE_NOTES_USER_EN.md", "RELEASE_NOTES_USER_KO.md"} {
				for _, suffix := range []string{"", ".bak"} {
					got, err := os.ReadFile(filepath.Join(dir, name+suffix))
					if err != nil || string(got) != "original "+name+suffix {
						t.Errorf("%s%s changed: %q, %v", name, suffix, got, err)
					}
				}
			}
		})
	}
}

func TestReleaseNotesFailureHelper(t *testing.T) {
	scenario := os.Getenv("RELEASE_NOTES_TEST_SCENARIO")
	if scenario == "" {
		return
	}
	key := ""
	if scenario == "generation" {
		key = "test-key"
		http.DefaultTransport = failingReleaseTransport{}
	}
	runReleaseNotes(&config.Config{GeminiAPIKey: key})
}
