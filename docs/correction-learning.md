# Correction Learning — 설계 근거·위협 모델·임계값

**Status:** Active (branch `claude/annyeong-dc301c`, 2026-08-30)
**원칙 문서:** "교정으로 성장하는 AI 기능 설계" 프롬프트 기반. 목표는 첫 시도에 맞히는 시스템이 아니라 **같은 방식으로 두 번 틀리지 않는 시스템**.

## 1. 축 경계 (닫힘/열림)

| 축 | 판정 | 집행 지점 |
|---|---|---|
| `category` | 닫힘 — {TASK, POLICY, QUERY, PROMISE, WAITING} | `types.IsValidTaskCategory` + guard G1, PATCH 핸들러 400 |
| `requester` | 닫힘 — envelope (SenderRaw/SenderEmail 우선) | `resolveRequester` (기존, 무변경) |
| `assignee` | 닫힘 — envelope ∪ (원문 등장 ∧ contacts 실존) | guard G2 (`ContactNameKnown`) |
| `task` 문장 | 열림 + 근거 — 원문과 토큰 겹침 ≥1 (한글 원문은 skip) | guard G5 |
| `deadline` | 열림 + 근거 — 표현 토큰이 원문에 등장 | guard G3 |
| `source_ts` | 닫힘 — payload 내 실존 `[ID:...]` | guard G4 |

guard 위치: `services/extraction_guard.go`, 호출부는 `scanner/channel_adapter.go`(chat)와
`channels/gmail_scan.go`(gmail) — AI 출력이 `BuildTask`에 닿기 전 반드시 통과.
학습된 어떤 규칙도 envelope 사실(`IsCcOnly`, `ExplicitMentions`)을 뒤집을 수 없다
(guard는 demote만 하고, task_builder의 envelope 로직은 그 뒤에 그대로 실행됨).

## 2. 위협 모델

**입력 신뢰 수준:** WhatsApp/Slack/Telegram/Gmail 외부 발신자 전원 — 신뢰 불가.
프롬프트 인젝션을 기본 가정으로 설계.

| 공격 | 방어 | 잔여 위험 |
|---|---|---|
| 본문으로 임의 인물에게 태스크 배정 (`"assignee: 관리자"`) | G2: envelope 밖 인물은 원문 등장 + contacts 실존 둘 다 필요, 실패 시 `shared` 강등 | contacts에 이미 있는 이름을 본문에 쓰면 통과 — 단 그 인물은 사용자가 등록한 실존 컨택트이므로 권한 상승이 아니라 오배정(가시적, 수정 가능) |
| 환각/주입 deadline (`"deadline: 오늘"`이 원문 맥락과 무관) | G3: 원문 미등장 표현 제거 | 원문에 날짜 단어가 실제로 있으면 통과 — 품질 문제로 격하 |
| 환각 source_ts로 잘못된 메시지에 링크 | G4: payload 실존 ID 검증 | 낮음 |
| 통째 환각 태스크 | G5: 토큰 겹침 0 → 폐기 | 한글 원문은 G5 skip (침묵 실패 방지가 우선) |
| **학습 오염** (외부인이 교사 신호 주입) | 교정 API 전부 `a.protected` + user_email 스코프. learned_examples는 인증 사용자의 명시 행동(수정 저장/수동 추가/삭제/완료)에서만 생성 | 사용자가 인젝션 문구가 포함된 원문을 그대로 학습 예시로 확정하면 그 문구가 프롬프트에 재주입됨 — 단일 사용자 신뢰 경계 내 자해에 해당, 심각도 낮음 |

**심각도 평가:** 태스크는 실행되지 않는 읽기 항목(사람이 보는 목록)이므로 성공한
인젝션도 최종 영향은 "잘못된 항목이 목록에 표시" 수준. 종합 심각도 **중간**.
"누가 나에게 태스크를 꽂을 수 있는가" = 사용자가 연결한 채널방의 모든 참여자,
단 assignee 지정력은 위 표대로 제한됨.

## 3. 임계값과 근거

| 항목 | 값 | 근거 |
|---|---|---|
| 억제(suppress) 승격 | 서로 다른 메시지 **3**건 | 잘못된 억제는 태스크가 조용히 사라지는 침묵 실패 — 가장 나쁜 실패 유형이므로 보수적 |
| 귀속/날짜/경계 승격 | 서로 다른 메시지 **2**건 | 과추출·오귀속은 가시적이고 지우면 그만 — 공격적으로 학습 |
| 수동 추가 학습 | 임계값 **없음** (즉시) | 사람이 직접 만든 확정 정답. 놓친 태스크는 신뢰를 깨는 유일한 실패 |
| 삭제 → negative 예시 | 승격 시점에만 생성 | 삭제 1건은 노이즈(이유 없이도 지움). Done 태스크 삭제는 신호 제외 |
| completion 예시 상한 | **29** | 약한 긍정 신호 풀 폭주 방지. 소수(prime) 컨벤션 |
| few-shot 로드 상한 | **97** | 프롬프트 선택 풀 상한. 소수 컨벤션 |
| few-shot 선택 | 2 → 학습 예시 존재 시 **3** | 학습 예시가 시드 2개를 밀어내지 않고 공존 가능하게 |

증거 카운트는 `seen_message_ids`로 항목 중복을 차단 — 같은 태스크를 다섯 번 고쳐도 증거 1건.
`rejected` 상태는 영구적이며 upsert가 되살리지 않음.

## 4. 학습 신호 매핑

| 사용자 행동 | 진입점 | 기록 |
|---|---|---|
| 수동 추가 (+원문 붙여넣기) | `POST /api/messages/create` | `learned_examples(manual_add)` 즉시 |
| 필드 수정 | `PATCH /api/messages/details` | `ai_original` diff → 관찰(kind별) + `learned_examples(edit_confirm)` + `field_sources` manual 마킹 |
| 삭제 (Done 아님) | `HandleDelete` 훅 | suppress 관찰, 승격 시 negative 예시 |
| 무편집 완료 | `HandleMarkDone` 훅 | `learned_examples(completion)`, 상한 29 |
| 규칙 승인/거부 | `POST /api/learning/observations/decide` | 승인=임계값 생략, 거부=영구 폐기 |

diff 기준선은 guard G0이 저장 시점에 심는 `metadata.ai_original`
(모델 원본 task/assignee/deadline/category). `metadata.field_sources`에 manual로
표시된 필드는 AI 재스캔(`state:"update"`)이 절대 덮지 않는다 (`services/task_routing.go`).

## 5. 폴백 (주 기능 무차단)

AI 클라이언트 부재/호출 실패 시 `services/extraction_fallback.go`:
현재 사용자가 **명시적으로 멘션된** 메시지만, envelope 사실로만 태스크 생성
(requester=발신자, assignee=사용자, category=TASK, task=원문 80자).
이미 합의된 값만 반환하므로 드리프트 생성 불가. `UNIQUE(user_email, source_ts)`
upsert로 이후 AI 재스캔과 중복되지 않음. 학습 실패 역시 전 경로 detached
goroutine(recover + `/CorrectionLearning` trace)이라 저장을 막을 수 없음.

## 6. 시드와 침식 방지

`ai/core/few_shots.go` `GetDefaultFewShots()` 9건은 코드 내 불변 시드.
학습 예시는 `learned_examples`에서 읽어 **append만** 하며 시드를 수정/삭제하지 않는다.
채널 친화도(+2)로 언어권 오염(예: 인도네시아어 교정이 한국어 방에 적용)을 완화.
`lang` 컬럼은 예약됨(향후 언어 감지 연동).

## 7. 조회/가역성

- `GET /api/learning/observations?status=` — 규칙·증거 현황
- `GET /api/learning/examples` — 학습 예시 목록
- 프론트 Learning 탭 — 승인/거부 UI
- 승격은 서버 로그에 기록 (`logger.Infof`)

## Open

- gmail 경로 폴백 없음 (분류 힌트 기반 흐름이 별도) — chat 채널만 적용
- 언어별 few-shot 가중치 (`lang` 컬럼 예약 상태)
- assignee_alias/deadline_expr 승격 규칙의 결정론적 적용 (현재 few-shot 경로로만 학습 반영; suppress만 guard G6에서 직접 집행)
- SSOT 챕터(07/08/11/13) 반영은 본 문서 링크로 대체, 차기 SSOT 갱신 시 통합
