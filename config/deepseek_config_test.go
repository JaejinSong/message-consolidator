package config

import "testing"

func TestLoadConfigDeepSeekDefaults(t *testing.T) {
	for _, k := range []string{
		"AI_PROVIDER", "DEEPSEEK_API_KEY", "DEEPSEEK_BASE_URL",
		"DEEPSEEK_FILTER_MODEL", "DEEPSEEK_ANALYSIS_MODEL",
		"DEEPSEEK_TRANSLATION_MODEL", "DEEPSEEK_REPORT_MODEL",
	} {
		t.Setenv(k, "")
	}

	cfg := LoadConfig()
	if cfg.AIProvider != "gemini" {
		t.Errorf("AIProvider default = %q, want gemini", cfg.AIProvider)
	}
	if cfg.DeepSeekBaseURL != "https://ollama.com/v1" {
		t.Errorf("DeepSeekBaseURL default = %q", cfg.DeepSeekBaseURL)
	}
	if cfg.DeepSeekFilterModel != "deepseek-v4.1-flash" || cfg.DeepSeekAnalysisModel != "deepseek-v4.1-flash" || cfg.DeepSeekTranslationModel != "deepseek-v4.1-flash" {
		t.Errorf("DeepSeek flash-tier model defaults = %q/%q/%q", cfg.DeepSeekFilterModel, cfg.DeepSeekAnalysisModel, cfg.DeepSeekTranslationModel)
	}
	if cfg.DeepSeekReportModel != "glm-5.3-flash" {
		t.Errorf("DeepSeekReportModel default = %q, want glm-5.3-flash", cfg.DeepSeekReportModel)
	}
}

func TestRegistryDeepSeekAndProvider(t *testing.T) {
	key := FindDef("DEEPSEEK_API_KEY")
	if key == nil {
		t.Fatal("DEEPSEEK_API_KEY not registered")
	}
	if !key.Secret || !key.RestartRequired {
		t.Errorf("DEEPSEEK_API_KEY must be Secret + RestartRequired, got Secret=%v RestartRequired=%v", key.Secret, key.RestartRequired)
	}

	prov := FindDef("AI_PROVIDER")
	if prov == nil {
		t.Fatal("AI_PROVIDER not registered")
	}
	if err := ValidateSetting(prov, "deepseek"); err != nil {
		t.Errorf("ValidateSetting(AI_PROVIDER, deepseek) = %v, want nil", err)
	}
	if err := ValidateSetting(prov, "gemini"); err != nil {
		t.Errorf("ValidateSetting(AI_PROVIDER, gemini) = %v, want nil", err)
	}
	if err := ValidateSetting(prov, "openai"); err == nil {
		t.Error("ValidateSetting(AI_PROVIDER, openai) should reject unknown provider")
	}
}

func TestLoadConfigClef(t *testing.T) {
	for _, k := range []string{"FILTER_PROVIDER", "CLEF_API_KEY", "CLEF_BASE_URL", "CLEF_MODEL", "CLEF_THRESHOLD"} {
		t.Setenv(k, "")
	}
	t.Setenv("DEEPSEEK_API_KEY", "ds-key")

	cfg := LoadConfig()
	if cfg.FilterProvider != "llm" || cfg.ClefBaseURL != "https://ollama.com/v1" || cfg.ClefModel != "clef:cloud" || cfg.ClefThreshold != 0.3 {
		t.Errorf("defaults = %q/%q/%q/%v", cfg.FilterProvider, cfg.ClefBaseURL, cfg.ClefModel, cfg.ClefThreshold)
	}
	if cfg.ClefAPIKey != "ds-key" {
		t.Errorf("ClefAPIKey fallback = %q, want DEEPSEEK_API_KEY value", cfg.ClefAPIKey)
	}

	t.Setenv("CLEF_API_KEY", "clef-key")
	if got := LoadConfig().ClefAPIKey; got != "clef-key" {
		t.Errorf("ClefAPIKey = %q, want clef-key", got)
	}
}

func TestLoadConfigClefThresholdValidation(t *testing.T) {
	cases := map[string]float64{"0.5": 0.5, "0": 0.3, "1": 0.3, "-0.2": 0.3, "abc": 0.3, "1.5": 0.3, "NaN": 0.3, "Inf": 0.3}
	for in, want := range cases {
		t.Setenv("CLEF_THRESHOLD", in)
		if got := LoadConfig().ClefThreshold; got != want {
			t.Errorf("CLEF_THRESHOLD=%q -> %v, want %v", in, got, want)
		}
	}
}
