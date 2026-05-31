# Plan: Commitment Tracking (Deadline Normalization + Aging Nudges + Commitments View)

> Status: TODO  
> Created: 2026-05-31

## Overview

Deadline 없는 PROMISE/WAITING이 추적 없이 사라지는 문제를 5개 기능으로 해결:
1. Deadline ISO 정규화 (`deadline_date` 컬럼 + normalizer)
2. 기한 없는 약속 에이징 nudge (D+3/7/14 Slack DM)
3. 일일 다이제스트 Commitment 섹션
4. `/api/commitments` 엔드포인트
5. 프론트엔드 Commitments 탭

## Steps

| Step | Status | File | Change | Model | Reason |
|------|--------|------|--------|-------|--------|
| 1 | DONE | `store/queries/schema.sql` | `messages` 테이블에 `deadline_date DATE`, `deadline_inferred INTEGER DEFAULT 0` 추가. `v_messages` 뷰에 두 컬럼 추가 (SELECT 끝에 append) | sonnet | 모든 이후 날짜 연산의 기반 |
| 2 | DONE | `store/migrations.go`, `store/db.go` | schemaVersion 9→10. `addDeadlineColumns()` 추가 (pragma_table_info 존재 체크, idempotent ALTER TABLE). `runFullDDL`에 `rebuildViews` 전에 연결 | sonnet | DDL 마이그레이션; 뷰 재빌드 전 컬럼 존재해야 함 |
| 2a | DONE | `store/migrations.go` | `suppressOldUndatedNudges()` 추가: created_at < (now - 14days) 인 PROMISE/WAITING done=0 항목의 metadata에 `undated_d3/d7/d14` 플래그 일괄 설정. `runFullDDL` 안에서 `addDeadlineColumns` 직후 호출. idempotent (이미 플래그 있으면 skip) | sonnet | 첫 배포 시 기존 stale 항목 nudge 스팸 방지 |
| 3 | DONE | bash | `sqlc generate` → `git diff --stat db/` 확인, 의도치 않은 변경 `git checkout` 원복 | bash | 자동생성 게이트 |
| 4 | DONE | `services/deadline_normalizer.go` (NEW) | `ParseDeadline(raw string, ref time.Time) (iso string, inferred bool)`: ISO 검증 통과 → (ISO, false); 자연어 파싱("today/tomorrow/next Friday/this week/EOD/by Mon") → (ISO, true); 불가 → ("", false) | sonnet | 순수 함수, 단위 테스트 가능; 프롬프트 변경의 safety net |
| 5 | DONE | `services/deadline_normalizer_test.go` (NEW) | 테이블 드리븐: ISO passthrough, 자연어 케이스, 빈값/garbage → ("",false), 참조날짜 경계 | sonnet | TDD; parser 정확도 |
| 6 | DONE | `store/types.go` | `ConsolidatedMessage`에 `DeadlineDate string`, `DeadlineInferred bool` 추가. `TodoItem`에도 동일 | inline | ≤40 LOC 필드 추가 |
| 7 | DONE | `services/task_builder.go` | `BuildTask` 내부에서 `ParseDeadline(p.Item.Deadline, p.Timestamp)` 호출 → `DeadlineDate`/`DeadlineInferred` 채움. 기존 `Deadline` raw 유지 (하위 호환) | sonnet | 단일 진입점에 정규화 집중 |
| 8 | DONE | `store/message_store.go` | `toCreateMessageParams`에 `DeadlineDate`/`DeadlineInferred` 매핑. `v_messages` 스캔 전체에 두 컬럼 append | sonnet | insert + scan path 일치 |
| 9 | DONE | `store/queries/messages.sql` | `CreateMessage`/`SaveMessagesBase` 컬럼+VALUES 추가. view-backed SELECT에 두 컬럼 append | sonnet | insert/select 계약; 컬럼 순서 Step 8 스캔과 일치 |
| 10 | DONE | bash | `sqlc generate` → `go build ./...` | bash | 컴파일 게이트 |
| B11-1 | DONE | `store/queries/messages.sql` | `SelectUndatedCommitments :many` 추가: `category IN ('PROMISE','WAITING')`, `deadline_date IS NULL`, `(deadline IS NULL OR deadline='')`, `done=0`, `is_deleted=0` | haiku | SelectDueSoon 패턴 복사, 단일 쿼리 블록 |
| B11-2 | DONE | `store/queries/messages.sql` | `SelectCommitments :many` 추가: user_email + view 파라미터(mine/waiting). PROMISE(assignee=me) or WAITING(requester=me). deadline, deadline_date, created_at 포함 | haiku | 뷰 SELECT 패턴 복사 |
| 12 | DONE | bash | `sqlc generate` (B11-1, B11-2 후) | bash | 자동생성 게이트 |
| 13 | DONE | `store/reminder_store.go` | `UndatedCommitment` row struct + `SelectUndated()` 래퍼. 기존 `HasReminded`/`MarkReminded`에 키 `undated_d3/d7/d14` 사용 | sonnet | store 래퍼; 기존 dedup 메커니즘 재사용 |
| 14 | DONE | `services/reminder_service.go` | `DispatchUndated(ctx) error` 추가: undated rows 로드 → created_at 기준 경과일 계산 → {3,7,14}일 임계값 돌파 시 Slack DM + `MarkReminded`. `formatUndatedNudgeText` 추가. Nil-safe | sonnet | 핵심 nudge 로직; 에이징+dedup 조합 |
| 15 | DONE | `services/reminder_service_test.go` (extend/NEW) | D+2 no-send, D+3 send+mark, 이미 marked skip, D+7/14 crossing, SlackID 없음 skip, SendDM 실패 → no mark | sonnet | TDD; threshold/dedup 경계 |
| 16 | DONE | `scanner/scanner_reminder.go` + test | `reminderDispatcher` 인터페이스에 `DispatchUndated(ctx) error` 추가. `runDeadlineReminder`에서 양쪽 호출(log+continue). fakeDispatcher 구현 업데이트 | sonnet | 인터페이스 변경 + 테스트 더블 |
| 17 | DONE | `services/daily_digest_service.go` | "내 약속 (기한 없음)" (PROMISE, assignee=me) + "기다리는 것 (기한 없음)" (WAITING, requester=me) 섹션 추가. canonical 필드로 identity 매칭 | sonnet | 기존 digest 파이프라인 확장 |
| 18 | DONE | `services/daily_digest_service_test.go` (extend) | 섹션 존재 여부, PROMISE/WAITING 버킷 분리, 빈 시 생략 | sonnet | TDD |
| 19 | DONE | `handlers/handlers_commitments.go` (NEW), `handlers/routes.go` | `HandleGetCommitments`: auth email, `view=mine|waiting`, `store.SelectCommitments`, overdue/undated/active 그룹화, `respondJSON`. `GET /api/commitments` 라우트 등록 | sonnet | 새 엔드포인트; 기존 handler 패턴 준수 |
| 20 | DONE | `handlers/handlers_commitments_test.go` (NEW) | view param 라우팅, 3그룹 파티셔닝, auth-email 스코핑, default view | sonnet | TDD |
| 21 | DONE | `ai/core/prompts/chat_system.prompt`, `gmail_system.prompt`, `notion_system.prompt` | deadline 규칙 강화: "ISO YYYY-MM-DD ONLY. 날짜 불명확 → empty string. 자연어 금지". inline 예시도 ISO로 교체 | haiku | 프롬프트 편집, Step 4가 safety net |
| B22-1 | DONE | `src/types.ts` | `Message`에 `deadline_date?: string`, `deadline_inferred?: boolean` 추가 | inline | ≤40 LOC |
| B22-2 | DONE | `src/locales/{en,ko,id,th}.ts` | commitment 탭 + 섹션 레이블 추가 (overdue/undated/active, "내 약속 (기한 없음)", "기다리는 것") | haiku | 4개 로케일 파일 패턴 복사 |
| 23 | DONE | `src/api.ts` | `fetchCommitments(view: 'mine'|'waiting')` 추가 → `GET /api/commitments?view=...` | haiku | 기존 fetch helper 패턴 복사 |
| 24 | DONE | `src/logic/commitments.ts` (NEW) | 순수 그룹핑/에이징 헬퍼: overdue/undated-aging/active 분류. 백엔드 로직과 동일 기준. `TaskTab`에 `'commitments'` 추가 | sonnet | FE 로직, 백엔드와 파리티 |
| 25 | DONE | `src/logic/commitments.test.ts` (NEW) | overdue 경계(today), undated aging D3/7/14, active 분류 단위 테스트 | sonnet | TDD |
| 26 | DONE | `src/app.ts`, `src/renderer.ts`, `src/state.ts` | Commitments 탭 버튼 + 활성화 시 fetchCommitments + 🔴/🟡/⚪ 그룹 렌더링. state 슬라이스 추가 | sonnet | 크로스파일 UI 배선 |
| 26a | DONE | `src/renderer.ts` (또는 deadline 렌더 담당 파일) | 모든 탭의 deadline 표시 로직에 `deadline_inferred` 체크 추가: true면 `~YYYY-MM-DD` 렌더링 + `title="AI 추론 날짜"` tooltip attribute | haiku | 기존 카드 렌더러 패턴 복사, 조건 분기 1개 추가 |
| 27 | DONE | bash | `go test ./...` + `make` + `npx tsc --noEmit` + `vitest run` | bash | 최종 검증 게이트 |

## Feature 6: 진행 지연 요청 감지 (Stalled Request Detection)

| Step | Status | File | Change | Model | Reason |
|------|--------|------|--------|-------|--------|
| R1 | DONE | `store/queries/messages.sql` | `SelectStalledRequests :many` 추가: category='TASK', done=0, is_deleted=0, `(julianday('now') - julianday(COALESCE(NULLIF(updated_at,'1970-01-01T00:00:00Z'), created_at))) >= ?` (threshold=3). SELECT id, user_email, task, requester, assignee, requester_canonical, assignee_canonical, updated_at, created_at, room, source, link. ORDER BY 경과일 DESC | sonnet | 진행 지연 핵심 쿼리; updated_at 기준 (없으면 created_at fallback) |
| R2 | DONE | bash | `sqlc generate` | bash | 자동생성 게이트 |
| R3 | DONE | `store/stalled_store.go` (NEW) | `StalledRequest` row struct + `SelectStalled(ctx, userEmail, thresholdDays int) ([]StalledRequest, error)` 래퍼. 결과를 두 버킷으로 분류: `Mine` (requester_canonical=me), `Observed` (그 외 — others/shared 관찰) | sonnet | store 래퍼; 버킷 분류 로직 |
| R4 | DONE | `store/stalled_store_test.go` (NEW) | Mine/Observed 버킷 분류 정확성, threshold 경계(D+2 미포함/D+3 포함), done=1 제외 | sonnet | TDD |
| R5 | DONE | `handlers/handlers_commitments.go` | `/api/commitments` 응답에 `stalled: {mine: [], observed: []}` 필드 추가. `SelectStalled` 호출, threshold config에서 읽기 (default 3) | sonnet | API 확장; 기존 응답 구조 extend |
| R6 | DONE | `services/daily_digest_service.go` | "진행 지연 요청" 섹션 추가. Mine 버킷: "📌 [task] (Y에게, D+N일)" / Observed 버킷: "👁 X → Y: [task] (D+N일째 업데이트 없음)". 각 5건 상한 (digest spam 방지) | sonnet | 일일 가시성 확보; Mine과 Observed 명확히 구분 |
| R7 | DONE | `src/logic/commitments.ts` | `groupStalled(items)` — Mine/Observed 재그룹, 경과일 계산, 정렬 | haiku | FE 로직; 백엔드 버킷과 파리티 |
| R8 | DONE | `src/renderer.ts` (또는 commitments 렌더 담당) | Commitments 탭 하단에 "진행 지연" 섹션: Mine은 🔴, Observed는 👁. "X → Y" 형식 명시, 경과일 뱃지 | sonnet | UI 렌더링 |

## Feature 7: 참조 탭 명확화 (Reference View Clarity)

| Step | Status | File | Change | Model | Reason |
|------|--------|------|--------|-------|--------|
| V1 | DONE | `services/tasks.go` | `CategoryOthers`("others")를 `CategoryReference`("reference")로 rename. IsCcOnly → shared 유지, CC가 아닌 순수 관찰자(X→Y 제3자) → reference 분리 | sonnet | others가 CC와 제3자 관찰을 뭉뚱그려 X→Y 관계가 안 보이는 문제 해결 |
| V2 | DONE | `src/locales/{en,ko,id,th}.ts` | `reference` 탭 레이블 추가 ("참조", "Reference", etc.) | haiku | 4개 로케일 패턴 복사 |
| V3 | DONE | `src/app.ts`, `src/renderer.ts` | `others` → `reference` 탭 대체. Commitments 탭의 Observed 섹션과 연결 (참조 탭 클릭 → 해당 X→Y 항목으로 이동) | sonnet | UI 배선 |
| V4 | DONE | bash | `go test ./...` + `make` + `npx tsc --noEmit` | bash | 최종 검증 게이트 |

## Dependencies

```
1 → 2 → 2a → 3                     (schema → migration → backfill → codegen)
4, 5 독립                           (순수 parser, DB 불필요)
6 독립
7 requires 4, 6
8, 9 requires 1, 6; 10 requires 8,9
B11-1, B11-2 requires 1 (병렬 가능); 12 requires B11-1+B11-2
13 requires 12(B11-1)
14 requires 13, 4
15 requires 14
16 requires 14
17 requires 13
18 requires 17
19, 20 requires 12(B11-2)
21 독립 (4 있으면 safety net 확보)
B22-1, B22-2 병렬 독립
23 requires B22-1
24, 25 독립
26 requires 23, 24, 25; 26a requires 26
27 마지막 (Feature 1~5)

R1 → R2 → R3 → R4                  (stalled query → codegen → store → test)
R5 requires R3, 19                  (API 확장 — 기존 commitments handler 위에)
R6 requires R3                      (digest — store 직접 호출)
R7 requires R1(개념); R8 requires R7, R5

V1 독립 (rename only)
V2 requires V1
V3 requires V2, R8
V4 마지막 (Feature 6~7)
```

## Risks

| 리스크 | 완화 |
|---|---|
| view SELECT 컬럼 순서 변경 → Scan 위치 오류 | 두 컬럼을 모든 SELECT projection 끝에 append; `go build`(Step 10)가 mismatch 감지 |
| SQLite DATE 타입 → 자연어 leftovers 비교 오류 | deadline_date만 비교, raw deadline 절대 비교 금지 |
| AI가 여전히 자연어 deadline 출력 | ParseDeadline fallback이 authoritative; deadline_inferred=1로 UI 구분 |
| 첫 배포 시 기존 undated 항목 D+3 일괄 발사 | 백필 정책 user 결정 필요 (Open Item) |

## Resolved Decisions

| 항목 | 결정 | 근거 |
|---|---|---|
| 기존 undated 백필 정책 | created_at ≥ 14일 → 모든 window(d3/d7/d14) suppressed 처리 (Step 2a). created_at < 14일 → 에이징 흐름 자연 탑승 | 스팸 방지 + 최근 약속은 정상 nudge |
| deadline_inferred UI 표시 | 모든 탭의 deadline 표시에 `~` 프리픽스 + tooltip "AI 추론 날짜" (Step 26a) | subtle 전체 일관성; Commitments 탭 한정 아님 |

## Success Criteria
- [ ] PROMISE "next Friday" → `deadline_date` ISO 저장, `deadline_inferred=1`
- [ ] Migration v10 idempotent 재실행, row 손실 0
- [ ] Undated PROMISE/WAITING → D+3/7/14 각 1회 DM, dedup 확인
- [ ] Daily digest에 두 섹션 (있을 때만 표시)
- [ ] `/api/commitments` overdue/undated/active 정확히 파티셔닝
- [ ] Commitments 탭 🔴/🟡/⚪ 렌더링
- [ ] AI 추론 deadline은 모든 탭에서 `~YYYY-MM-DD` + tooltip으로 표시
- [ ] 기존 14일+ 항목은 첫 배포 시 nudge 발송 없음 (suppressed 확인)
- [ ] Stalled Mine: 내가 시킨 TASK 중 D+3 이상 업데이트 없는 것 표시
- [ ] Stalled Observed: 관찰 중인 X→Y TASK 중 D+3 이상 업데이트 없는 것 표시
- [ ] "others" 탭이 "reference" 탭으로 전환, X→Y 관계 명확히 표시
- [ ] `go test ./...` + `make` + FE tsc+vitest 모두 green, 신규 로직 80%+ 커버리지

## Loop Prompt

```
Commitment Tracking 플랜(`docs/plans/commitment-tracking.md`)의 다음 TODO 스텝을 구현하라.

매 iteration 규칙:
1. 플랜 파일 읽기 → Status가 TODO이고 dependency가 모두 DONE인 가장 낮은 번호 스텝 선택.
   B-prefix 동일 batch + 파일 충돌 없으면 함께 진행 가능.
2. Go 심볼 편집 전 Serena find_symbol/get_symbols_overview 호출 (CLAUDE.md 규칙).
   db/*.sql.go 직접 수정 금지 — sqlc generate 만 사용.
3. 해당 스텝의 Change 정확히 구현. 제약: ctx first param, fmt.Errorf wrap,
   인터페이스 consumer 정의, view 컬럼은 끝에 append.
4. 검증:
   - schema/query/codegen → sqlc generate + go build ./...
   - Go 로직 → go build ./... + go test ./<pkg>/...
   - bash 스텝 → 명시된 명령 실행
   - FE → npx tsc --noEmit (+ vitest run <file> 테스트 추가 시)
5. 검증 실패 → 구현 수정 (테스트가 틀린 경우만 테스트 수정).
   Handler→Service→Store 위반 또는 모호함 → STOP 후 보고.
6. 플랜 파일의 해당 스텝 Status를 DONE으로 업데이트.
7. 스텝 완료 보고 (스텝 번호, 터치한 파일, 검증 명령 + 결과).
   하나의 스텝이 green이면 중단 — 무관한 스텝 묶지 말 것.

Step 27 완료 또는 블로커 발생 시 루프 종료.
```
