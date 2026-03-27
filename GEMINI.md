# Gemini Configuration (v2.1.982)

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
    - 모든 신규 UI는 BEM(`c-block__element--modifier`) 규칙을 필수적으로 준수한다.
    - 하드코딩된 값(px, hex) 사용을 금지하며, `variables.css`의 토큰 사용을 강제한다.
    - 배포 전 `node verify-css.cjs`를 실행하여 정합성을 최종 검증한다.

## Monitoring (WhaTap)
- **Agent**: Go/Browser Agent 적용 (`sessionReplaySampleRate: 100`)
- **Resource**: 모니터링 에이전트로 인한 **150MB** 메모리 증분 인지 필요.