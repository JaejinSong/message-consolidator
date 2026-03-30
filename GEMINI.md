# Gemini Configuration (v2.1.983)

## Basic Specs
- **Model**: `gemini-3-flash-preview` (Main), `gemini-3.1-flash-lite-preview` (Translation)
- **Role**: Analysis, Task Extraction, 


## Token Optimization Strategy (TEP) 
> [!IMPORTANT]
> 토큰 소모를 최소화하면서 논리적 정합성과 품질을 유지합니다.

1. **Surgical Read**: `view_file` 시 항상 `StartLine`, `EndLine`을 명시하여 필요한 범위(100~200라인 내외)만 로드한다. 위치 식별은 `grep`을 선행한다.
2. **Artifact Slimming**: `task.md`의 완료 항목은 1줄 요약 후 아카이브하며, 중복된 사용자 요청은 아티팩트에서 제외한다. 
3. **KI Leverage**: 복잡한 아키텍처나 분석 결과는 `knowledge/`에 저장하고 이후 참조만 하여 분석 비용을 절감한다.
4. **Logic-First Verification**: 브라우저 기반 테스트를 지양하고 `Node.js`/`Go` 스크립트를 통한 논리 검증을 우선한다.
5. **Bug-Fix-Test Mandate**: 모든 버그 수정 시 해당 버그의 재발 방지를 위한 **독립적 테스트 케이스**를 반드시 추가한다. (Rule 1.1 준수)
6. **Dry Protocol**: 모든 답변과 아티팩트는 불필요한 서술 없이 기술적 팩트 위주로 압축하여 작성한다. (Rule 1.1 및 Rule 4 준수)
7. **CSS Design System Enforcement**:
    > [!IMPORTANT]
    > **하드코딩된 px, hex 값 사용은 절대 금지**하며, 반드시 `variables.css`의 토큰 또는 `rem` 단위를 사용한다. (`16px = 1rem` 기준)
    - 모든 신규 UI는 BEM(`c-block__element--modifier`) 규칙을 필수적으로 준수한다.
    - **가독성 및 레이아웃**: `rem` 단위를 우선하여 해상도 대응력을 높이고, 가로 여백은 `0.5rem~1rem` 범위를 준수한다.
    - **배포 전 검증**: 수정 후에는 반드시 `node verify-css.cjs`를 실행하여 정합성을 최종 확인하고, 실패 시 배포가 불가능함을 인지한다.

## Coding Guidelines
- **Self-Documenting Code First, Then Intent-Driven Comments**: Strive to write code that is self-documenting. Before adding a comment, first ask: "Can I make the code itself clearer?" If the code alone cannot express the 'why', then add a comment explaining the intent, not the implementation. All comments must be in English.
  - **Use Descriptive Naming**: Function names should be verb phrases describing their outcome (e.g., `calculateTotalScore`, not `procData`). Variable names should be clear nouns (e.g., `remainingTasks`, not `list`).
  - **Leverage Code Structure for Clarity**: Use language features to document intent. In Go tests, use `t.Run("should do X when Y", ...)` instead of procedural comments. Extract complex logic into small, well-named helper functions.
  - **Reserve Comments for 'The Why'**: Only add comments for things the code cannot say on its own. Good examples include:
    - Architectural trade-offs: `// Why: We use a standard context here because a failure in one channel scan should not cancel others.`
    - External constraints: `// Why: The external API has a rate limit of 5 req/sec, so we add a delay.`
    - Justifying "magic numbers": `// Why: Enforce a default of 6 days to prevent unbounded data growth if the config is missing.`

  - **(원칙) 코드로 설명하고, 불가할 때만 '왜'를 주석으로 단다**: 주석을 달기 전, "코드를 더 명확하게 만들 수 없을까?"를 먼저 고민한다.
    - **서술적인 이름 사용**: 함수 이름은 `calculateTotalScore`처럼 동사구로, 변수 이름은 `remainingTasks`처럼 명확한 명사로 짓는다.
    - **코드 구조로 명확성 높이기**: Go 테스트에서는 `t.Run("X일 때 Y를 해야 한다", ...)`을 사용하고, 복잡한 로직은 이름이 분명한 헬퍼 함수로 추출하여 코드의 의도를 드러낸다.
    - **'왜'에만 주석 달기**: 코드가 스스로 설명할 수 없는 내용에만 주석을 단다. (예: 아키텍처 결정, 외부 API 제약, 특정 상수의 존재 이유)

## Monitoring (WhaTap)
- **Agent**: Go/Browser Agent 적용 (`sessionReplaySampleRate: 100`)
- **Resource**: 모니터링 에이전트로 인한 **150MB** 메모리 증분 인지 필요.