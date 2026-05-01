# 종합 문서 / Comprehensive Documentation

**최종 갱신 / Last Updated:** 2026-04-30
**상태 / Status:** SSOT (Single Source of Truth)
**총 분량 / Total:** 23 챕터 + INDEX, 11,505줄

## 한국어

이 디렉토리는 `message-consolidator` 프로젝트의 **단일 사실 출처**입니다. 코드 위치, 도메인 모델, 데이터 계층, AI 파이프라인, 채널 어댑터, 운영 절차, 사용자 매뉴얼까지 모든 정보를 23개 챕터에 집대성했습니다. 이전 문서들(`CLAUDE.md`, `TECH.md`, `deploy.md`, `docs/USER_MANUAL.md`, `knowledge/BACKEND_ARCHITECTURE.md`, `knowledge/FRONTEND_ARCHITECTURE.md`, `RELEASE_NOTES_*` 4종)은 이 SSOT의 직전 스냅샷입니다.

## English

This directory is the **single source of truth** for the `message-consolidator` project. Code locations, domain model, data layer, AI pipelines, channel adapters, operations procedures, and user manual are consolidated into 23 chapters. Legacy docs (`CLAUDE.md`, `TECH.md`, `deploy.md`, `docs/USER_MANUAL.md`, `knowledge/*.md`, `RELEASE_NOTES_*`) are prior snapshots of this SSOT.

---

## 챕터 / Chapters

| # | 파일 | 줄 | 한 줄 (KO) | One-liner (EN) | 핵심 코드 / Key Code |
|---|---|---|---|---|---|
| 00 | [overview.md](00-overview.md) | 157 | 비전·스택·아키텍처 1-pager | Vision, stack, architecture | [main.go](../../main.go), [package.json](../../package.json) |
| 01 | [getting-started.md](01-getting-started.md) | 268 | 로컬 개발·빌드·테스트 | Local dev, build, test | [Makefile](../../Makefile), [unit-test.sh](../../unit-test.sh) |
| 02 | [domain-model.md](02-domain-model.md) | 508 | 메시지·태스크·컨택트·리포트 모델 | Message/Task/Contact/Report model | [internal/ids/](../../internal/ids/), [db/models.go](../../db/models.go) |
| 03 | [backend-architecture.md](03-backend-architecture.md) | 686 | DI·패키지·요청 플로우 | DI, packages, request flow | [main.go](../../main.go), [handlers/routes.go](../../handlers/routes.go) |
| 04 | [data-layer.md](04-data-layer.md) | 606 | sqlc·Turso·19 entity·migrations | sqlc, Turso, 19 entities, migrations | [store/](../../store/), [db/](../../db/), [store/queries/](../../store/queries/) |
| 05 | [channels.md](05-channels.md) | 405 | 4 채널 어댑터 (Slack/Gmail/Telegram/WhatsApp) | 4 channel adapters | [channels/](../../channels/) |
| 06 | [scanner-pipeline.md](06-scanner-pipeline.md) | 388 | 스캔 루프·enricher·prime-pool | Scan loop, enricher, prime-pool | [scanner/](../../scanner/) |
| 07 | [ai-filter-pipeline.md](07-ai-filter-pipeline.md) | 655 | Parser→Flash-Lite→Flash→Pro·RAG | 4-stage filter + RAG | [ai/](../../ai/) |
| 08 | [services-business-logic.md](08-services-business-logic.md) | 651 | 13 서비스 (tasks·consolidate·reports) | 13 service modules | [services/](../../services/) |
| 09 | [identity-and-dedup.md](09-identity-and-dedup.md) | 456 | DSU·alias·신원 매핑·중복 제거 | DSU, alias, identity, dedup | [ai/identity_resolver.go](../../ai/identity_resolver.go), [store/dsu.go](../../store/dsu.go) |
| 10 | [locking-and-concurrency.md](10-locking-and-concurrency.md) | 498 | 분산 락·safego·graceful shutdown | Lock, safego, shutdown | [services/lock_service.go](../../services/lock_service.go), [internal/safego/](../../internal/safego/) |
| 11 | [handlers-and-api.md](11-handlers-and-api.md) | 861 | 12 handler·68 라우트·미들웨어 | 12 handlers, 68 routes, middleware | [handlers/](../../handlers/) |
| 12 | [auth-and-security.md](12-auth-and-security.md) | 441 | Google OAuth·세션·AdminMiddleware | Google OAuth, session, admin | [auth/](../../auth/) |
| 13 | [frontend-architecture.md](13-frontend-architecture.md) | 735 | 모듈·이벤트·state·renderer | Modules, events, state, renderer | [src/app.ts](../../src/app.ts), [src/state.ts](../../src/state.ts) |
| 14 | [frontend-ui-system.md](14-frontend-ui-system.md) | 519 | CSS 토큰·BEM·components·i18n | CSS tokens, BEM, components, i18n | [static/css/](../../static/css/), [src/components/](../../src/components/) |
| 15 | [observability.md](15-observability.md) | 374 | WhaTap·logger 3종·ai_logger | WhaTap, 3 loggers, ai_logger | [logger/](../../logger/), [internal/whataphttpx/](../../internal/whataphttpx/) |
| 16 | [cli-and-tools.md](16-cli-and-tools.md) | 498 | 15 CLI 단위 (sim·verify·check·util) | 15 CLI tools | [cmd/](../../cmd/) |
| 17 | [testing-strategy.md](17-testing-strategy.md) | 371 | 88 Go 테스트·21 vitest·AI 회귀 | 88 Go, 21 vitest, AI regression | [tests/](../../tests/), [unit-test.sh](../../unit-test.sh) |
| 18 | [build-and-deploy.md](18-build-and-deploy.md) | 668 | Makefile 19 target·Docker·Caddy·VPS | 19 targets, Docker, Caddy, VPS | [Makefile](../../Makefile), [docker-compose.yml](../../docker-compose.yml), [Caddyfile](../../Caddyfile) |
| 19 | [user-manual.md](19-user-manual.md) | 575 | 최종 사용자 가이드 (채널 연동·FAQ) | End-user guide | (UI labels in [src/locales/](../../src/locales/)) |
| 20 | [operations-runbook.md](20-operations-runbook.md) | 526 | 트러블슈팅 10건·사고 대응 | 10 troubleshooting scenarios | — |
| 21 | [release-history.md](21-release-history.md) | 346 | v2.4.1~v2.4.6 통합·최근 50 commit | Releases consolidated | [RELEASE_NOTES_*](../../) |
| 99 | [glossary.md](99-glossary.md) | 313 | 40+ 용어 사전 (한/영) | 40+ term glossary (KO/EN) | — |

---

## 빠른 진입점 / Quick Entry Points

| 역할 / Role | 추천 순서 / Recommended path |
|---|---|
| 신규 개발자 / New developer | 00 → 01 → 03 → 02 → 04 |
| 백엔드 / Backend engineer | 02 → 03 → 04 → 06 → 07 → 08 → 11 |
| 프론트엔드 / Frontend engineer | 13 → 14 → 11 (API) |
| 운영 / SRE / Ops | 18 → 20 → 15 → 10 |
| 보안 / Security | 12 → 04 (token storage) → 15 |
| 사용자 / End user | 19 |
| 신원/매핑 작업 / Identity work | 09 → 02 → 07 |

---

## 주제별 인덱스 / Topical Index

**한국어:**

- **DI/부트스트랩**: 03 (시퀀스 다이어그램), 10 (shutdown)
- **AI 4단계 필터**: 07 (Parser → Flash-Lite → Flash → Pro)
- **Prime-Pool 부하 분산**: 06 (5개 prime), 10 (분산 락 결합)
- **Phantom Type ID 시스템**: 02, 09 (위반 사례)
- **sqlc 워크플로**: 04 (상세), 01 (Commands)
- **WhaTap APM 패턴 매트릭스**: 15 (7 영역)
- **Graceful Shutdown 시퀀스**: 10, 03
- **OAuth 흐름**: 12 (사용자 로그인), 05 (Gmail), 18 (.env)
- **회귀 테스트 (AI)**: 17, 16 (verify/*)
- **로그 3종 (logger/structured/ai)**: 15

**English:**

Same structure — see chapter cross-references inside each file.

---

## 갱신 정책 / Update Policy

**한국어:**

1. **코드와 문서 동시 변경**: 코드 PR과 같은 PR에서 영향받은 챕터를 갱신. 분리된 PR 금지.
2. **시점 표기**: 챕터 18(빌드/배포)과 21(릴리즈)에는 "최종 갱신: YYYY-MM-DD" 헤더 필수. 다른 챕터도 4분기에 한 번 sweep.
3. **검증 명령**: 머지 전 아래 3개 명령이 모두 0 출력이어야 함 (broken link/anchor 차단).
4. **레거시 문서**: `CLAUDE.md`/`TECH.md`/`deploy.md`/`docs/USER_MANUAL.md`/`knowledge/*.md`/`RELEASE_NOTES_*` 는 SSOT의 직전 스냅샷. 사용자 승인 후 별도 단계에서 redirect 헤더를 추가할 예정.
5. **새 챕터 추가**: 02-prefix 보존 (예: 22-new-topic.md). INDEX.md 표 + 빠른 진입점 + 주제별 인덱스 동시 갱신.
6. **작성 규칙**: 한/영 병기, repo-relative 코드 링크, WHAT 금지 WHY/HOW, 스니펫 ≤ 8줄, 모호 시 추측 금지.

**English:**

1. **Sync code and docs** in the same PR. No split PRs.
2. **Date headers** required on 18 and 21; quarterly sweep elsewhere.
3. **Verification** — all 3 commands below must produce empty output before merge.
4. **Legacy docs** are prior snapshots, redirect headers will be added separately after user approval.
5. **New chapters** preserve 02-prefix. Update INDEX table + Quick Entry + Topical Index simultaneously.
6. **Writing rules**: KO/EN bilingual, repo-relative links, no WHAT-only prose, snippets ≤ 8 lines, no guessing.

---

## 검증 명령 / Verification

```bash
cd /home/jinro/.gemini/message-consolidator/docs/comprehensive

# (a) 코드 경로 링크 존재 확인 / repo-relative link existence
grep -hoE '\]\(\.\./[^)#]+' *.md | sed 's|](||' | sort -u | \
  while read p; do [ -e "$p" ] || echo "MISSING: $p"; done

# (b) 챕터 간 링크 일관성 / chapter-to-chapter file existence
grep -hoE '\]\(([0-9]{2}-[a-z-]+\.md)' *.md | sed 's/](//' | sort -u | \
  while read f; do [ -f "$f" ] || echo "MISSING_FILE: $f"; done

# (c) 한/영 병기 카운트 일치 / KO and EN block count parity
for f in [0-9]*.md; do
  ko=$(grep -c '^\*\*한국어:\*\*' "$f")
  en=$(grep -c '^\*\*English:\*\*' "$f")
  [ "$ko" = "$en" ] || echo "MISMATCH $f: ko=$ko en=$en"
done
```

3개 모두 출력이 비어 있으면 통과 / All 3 must produce empty output to pass.

---

## 작성 메타데이터 / Authoring Metadata

**한국어:**

- **작성 방식**: Sonnet 4.6 sub-agent 20개 (Wave 1~4 순차, Wave 내부 병렬)
- **흡수된 레거시 문서**: 9개 (1,107줄) → 23 챕터 (11,505줄)
- **모델**: sub-agent는 sonnet, 최종 INDEX/glossary는 main(Opus)
- **WhaTap 부트스트랩 함정**: trace.Init 누락 시 silent no-op — chapter 15에 명시
- **알려진 stale (레거시 문서 대비)**: 챕터별 "Deltas from legacy docs" 섹션에 분산 기록

**English:**

- Authored via 20 Sonnet 4.6 sub-agents (Wave 1–4 sequential, intra-wave parallel)
- 9 legacy docs (1,107 lines) absorbed into 23 chapters (11,505 lines)
- Sonnet for chapters, Opus for INDEX/glossary
- WhaTap bootstrap pitfall (silent no-op when `trace.Init` missing) documented in chapter 15
- Known stale items vs legacy: see "Deltas from legacy docs" sections within each chapter

---

## 발견된 주요 Stale (레거시 문서 검토 결과) / Notable Stale Findings

| 출처 | 항목 | 이전 기술 | 현재 사실 | 관련 챕터 |
|---|---|---|---|---|
| TECH.md | Gemini 모델명 | "Gemini 3.1 Pro" 등 | `gemini-3-flash-preview`, `gemini-3.1-flash-lite` (config.go) | 07, 99 |
| TECH.md | Prime 루프 갯수 | "8 loop" | 실제 11개 (5 prime: 59/61/67/71/73s) | 06 |
| TECH.md | Slack polling 방식 | 미언급 | timestamp-based polling 채택 | 05 |
| TECH.md | Gmail History API | 미언급 | 미사용, timestamp checkpoint 사용 | 05 |
| TECH.md | WhatsApp 미디어 | 미언급 | 미구현 | 05 |
| BACKEND_ARCHITECTURE.md | logger 책임 | "APM 래퍼 제공" | 별도 (logger=레벨 로그, APM은 internal/whataphttpx) | 03, 15 |
| BACKEND_ARCHITECTURE.md | scanner AI 파이프 | "직접 전달" | scanner 자체 GeminiClient + 별도 handler용 AI | 03 |
| BACKEND_ARCHITECTURE.md | internal 패키지 | 미언급 | ids/safego/whataphttpx/testutil 4개 | 03 |
| deploy.md | 프론트 컨테이너 | "Caddy 이미지" | alpine sidecar (Caddy 없음) | 18 |
| deploy.md | DISABLE_STATIC_SERVING | 미언급 | docker-compose.yml에 존재 | 18 |
| deploy.md | entrypoint.sh WhaTap | 미언급 | Agent 기동 로직 포함 | 18 |
| (코드) | types/types.go SenderID | Phantom Type 룰 | int64 사용 (위반) | 09 |
| (코드) | language_deprecated 컬럼 | — | task/report_translations에 존재하나 미사용 | 04 |
| (코드) | store/migrations/*.sql | — | 3 파일 삭제됨, migrations.go만 사용 | 04 |
| (코드) | safego API | 가설 `safego.Go(ctx,fn)` | 실제 `safego.Recover(name)` 단일 | 10 |
| (코드) | handlers_misc.go | 11 handler로 알려짐 | 12번째 파일 존재 | 11 |
| (코드) | release_notes 자동화 | 미문서화 | mc-util release_notes.go 존재 | 16 |

---

## 라이선스 / 책임자 / License & Owner

- **저장소 / Repo**: `/home/jinro/.gemini/message-consolidator`
- **책임자 / Owner**: Jaejin Song (jjsong@whatap.io)
- **이슈 / Issues**: 코드 변경과 함께 챕터 갱신 의무. 누락 발견 시 INDEX 하단에 추가 기록.
