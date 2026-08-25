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
	if cfg.DeepSeekFilterModel != "deepseek-v4-flash:0731" || cfg.DeepSeekAnalysisModel != "deepseek-v4-flash:0731" || cfg.DeepSeekTranslationModel != "deepseek-v4-flash:0731" {
		t.Errorf("DeepSeek flash-tier model defaults = %q/%q/%q", cfg.DeepSeekFilterModel, cfg.DeepSeekAnalysisModel, cfg.DeepSeekTranslationModel)
	}
	if cfg.DeepSeekReportModel != "deepseek-v4-pro" {
		t.Errorf("DeepSeekReportModel default = %q, want deepseek-v4-pro", cfg.DeepSeekReportModel)
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
