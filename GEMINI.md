# Gemini Model Configuration

모델은 **Gemini 3 Flash Preview** (`gemini-3-flash-preview`)를 기본 모델로 사용한다.

## Configuration Details
- **Model ID**: `gemini-3-flash-preview`
- **Purpose**: Task extraction, translation, and analysis.

## Versioning Policy
- **보수적인 버전 넘버링**: 기능 추가나 구조 개선 시 버전 번호를 급격하게 올리기보다, 소수점 단위(Patch/Minor)로 보수적으로 업데이트한다. 패치/마이너 버전은 2~3 자리 단위를 사용한다. (예: 2.1.981) 버전 이력을 촘촘하게 관리한다.

## Token Optimization Strategy (Cost Saving)
- **Model Selection**: 
    - 분석(Analysis): `gemini-3-flash-preview` (정교한 태스크 추출 및 추론용)
    - 번역(Translation): `gemini-3.1-flash-lite-preview` (단순 번역은 Lite 모델로 비용 효율화)
- **Prompt Slimming**: 시스템 프롬프트를 핵심 요구사항 위주로 압축하여 불필요한 컨텍스트 토큰 소모를 줄인다. (최근 `ai/gemini.go` 반영 완료)
- **Batch Processing**: 여러 업무를 묶음 처리하여 고정 오버헤드 토큰 발생을 억제한다.

## 검증
- message-consolidator 는 개발환경인 jjsong-devmachine 에서는 실행하지 않는다. 실행을 통한 검증은 VPS 에서 수행한다.