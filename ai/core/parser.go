package core

import (
	"bytes"
	"errors"
	"strings"
	"text/template"
)

// ErrInvalidFrontmatter는 프롬프트 메타데이터 형식이 잘못되었을 때 반환됩니다.
var ErrInvalidFrontmatter = errors.New("invalid prompt format: must start with ---")

// PromptMeta는 프롬프트의 버전 및 프로바이더별 모델/thinking 라우팅 정보를 담습니다.
// Why: model/thinking은 프로바이더마다 달라서 프롬프트가 실행 방식을 자기기술하도록
// per-provider로 명시한다. 미설정(빈 문자열)이면 호출부가 코드 modelSpec으로 폴백.
type PromptMeta struct {
	Name             string
	Version          string
	GeminiModel      string
	GeminiThinking   string // "on" | "off" | "default" | "" (unset → code fallback)
	DeepSeekModel    string
	DeepSeekThinking string // "on" | "off" | "default" | ""
}

// ModelFor는 활성 프로바이더의 frontmatter 모델을 반환합니다(미설정 시 "").
func (m PromptMeta) ModelFor(provider string) string {
	if strings.EqualFold(provider, "deepseek") {
		return m.DeepSeekModel
	}
	return m.GeminiModel
}

// ThinkingFor는 활성 프로바이더의 frontmatter thinking 토큰을 반환합니다(미설정 시 "").
func (m PromptMeta) ThinkingFor(provider string) string {
	if strings.EqualFold(provider, "deepseek") {
		return m.DeepSeekThinking
	}
	return m.GeminiThinking
}

// ParsedPrompt는 런타임에 템플릿 엔진에 전달될 최종 객체입니다.
type ParsedPrompt struct {
	Meta PromptMeta
	Body string // 토큰 소모 대상 (Gemini API로 전송될 순수 텍스트)
}

// FewShot은 프롬프트 예시 데이터를 구조화합니다.
type FewShot struct {
	Input    string `json:"input"`
	Expected string `json:"expected"`
	Source   string `json:"source,omitempty"` // Why: channel affinity scoring in SelectFewShotsForSource; empty for seed shots.
	Lang     string `json:"lang,omitempty"`   // reserved: populated by future language detection
}

// Render는 주어진 컨텍스트를 사용하여 프롬프트 본문을 렌더링합니다.
// any 사유: text/template.Execute 시그니처와 동일 — 호출자별 임의 데이터 모델 수용.
func (p *ParsedPrompt) Render(data any) (string, error) {
	tmpl, err := template.New("prompt").Parse(p.Body)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ParsePrompt는 30라인 이내, 2 depth 제약을 준수하여 구현되었습니다.
func ParsePrompt(content string) (*ParsedPrompt, error) {
	// Guard Clause 1: 공백 제거 및 최상단 구분자 강제 검증
	cleanContent := strings.TrimSpace(content)
	if !strings.HasPrefix(cleanContent, "---") {
		return nil, ErrInvalidFrontmatter
	}

	// Guard Clause 2: SplitN 적용 및 가비지 데이터 배제
	parts := strings.SplitN(cleanContent, "---", 3)
	if len(parts) < 3 || strings.TrimSpace(parts[0]) != "" {
		return nil, ErrInvalidFrontmatter
	}

	// 메타데이터 파싱 및 바디 할당
	meta := parseMetadata(parts[1])
	return &ParsedPrompt{Meta: meta, Body: strings.TrimSpace(parts[2])}, nil
}

// parseMetadata는 메타데이터 문자열을 PromptMeta 구조체로 파싱합니다.
func parseMetadata(raw string) PromptMeta {
	meta := PromptMeta{}
	for _, line := range strings.Split(raw, "\n") {
		kv := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(kv) < 2 {
			continue
		}

		assignField(strings.ToLower(strings.TrimSpace(kv[0])), strings.TrimSpace(kv[1]), &meta)
	}
	return meta
}

// assignField는 frontmatter 키를 PromptMeta 필드에 매핑합니다. key는 소문자로 정규화되어 전달됩니다.
func assignField(key, val string, meta *PromptMeta) {
	switch key {
	case "name":
		meta.Name = val
	case "version":
		meta.Version = val
	case "geminimodel":
		meta.GeminiModel = val
	case "geminithinking":
		meta.GeminiThinking = val
	case "deepseekmodel":
		meta.DeepSeekModel = val
	case "deepseekthinking":
		meta.DeepSeekThinking = val
	}
}
