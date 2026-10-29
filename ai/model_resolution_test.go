package ai

import (
	"message-consolidator/ai/core"
	"testing"
)

// TestResolveModelThinking pins the model+thinking resolution for every
// model-resolution-source prompt. The frontmatter (deepseekModel/deepseekThinking) is
// authoritative; lite_filter's spec fallback is covered by its own frontmatter values.
func TestResolveModelThinking(t *testing.T) {
	type want struct {
		model    string
		thinking ThinkingMode
	}
	cases := []struct {
		name   string
		prompt core.PromptName
		spec   modelSpec // fallback when frontmatter omits a value
		deep   want
	}{
		{"chat_system", core.PromptChatSystem, modelSpec{}, want{"deepseek-v4.1-flash", ThinkOn}},
		{"gmail_system", core.PromptGmailSystem, modelSpec{}, want{"deepseek-v4.1-flash", ThinkOn}},
		{"notion_system", core.PromptNotionSystem, modelSpec{}, want{"deepseek-v4.1-flash", ThinkOff}},
		// Why: the report stage runs glm-5.3-flash on the same Ollama-cloud endpoint; the
		// deepseekModel key selects the model for provider=deepseek, not a DeepSeek-family id.
		{"report_summary", core.PromptReportSummary, modelSpec{}, want{"glm-5.3-flash", ThinkOn}},
		{"completion_check", core.PromptCompletionCheck, modelSpec{}, want{"deepseek-v4.1-flash", ThinkOn}},
		{"followup_check", core.PromptFollowupCheck, modelSpec{}, want{"deepseek-v4.1-flash", ThinkOn}},
		{"task_merge_summary", core.PromptTaskMergeSummary, modelSpec{}, want{"deepseek-v4.1-flash", ThinkOff}},
		{"translation_system", core.PromptTranslationSystem, modelSpec{}, want{"deepseek-v4.1-flash", ThinkOff}},
		{"report_translator", core.PromptReportTranslator, modelSpec{}, want{"deepseek-v4.1-flash", ThinkOff}},
		{"task_translator", core.PromptTaskTranslator, modelSpec{}, want{"deepseek-v4.1-flash", ThinkOff}},
		{"batch_translator", core.PromptBatchTranslator, modelSpec{}, want{"deepseek-v4.1-flash", ThinkOff}},
		{"identity_group_merge", core.PromptIdentityGroupMerge, modelSpec{}, want{"deepseek-v4.1-flash", ThinkOn}},
		{"lite_filter", core.PromptLiteFilter, modelSpec{model: "", thinking: ThinkDefault}, want{"deepseek-v4.1-flash", ThinkOff}},
	}

	ds := &AIClient{provider: providerDeepSeek}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			p := core.LoadPrompt(tc.prompt)
			if got := ds.resolveModel(p, tc.spec); got != tc.deep.model {
				t.Errorf("deepseek model = %q, want %q", got, tc.deep.model)
			}
			if got := ds.resolveThinking(p, tc.spec); got != tc.deep.thinking {
				t.Errorf("deepseek thinking = %v, want %v", got, tc.deep.thinking)
			}
		})
	}
}
