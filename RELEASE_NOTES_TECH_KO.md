> [!NOTE]
> **이 문서는 종합 문서(SSOT)의 직전 스냅샷입니다 / Prior snapshot superseded by SSOT.**
> 최신 정식 버전 / Latest authoritative version: [docs/comprehensive/INDEX.md](docs/comprehensive/INDEX.md)

# 업데이트 소식 (기술) - v2.4.10 (2026-10-28 UTC)

- [FIX] `GetMessageByID`와 `RefreshCacheActive`에서 마감일을 날짜 부분의 TEXT로 조회해 libsql 원격 드라이버의 `DATE` → `sql.NullTime` 변환 오류를 방지. NULL, 시간대 이동 없는 날짜 보존.
- [TEST] Hrana HTTP 드라이버 회귀 테스트와 SQLite 캐시 cold/warm 검증 통과.

---

# 업데이트 소식 (기술) - v2.4.9 (2026-10-28 UTC)

- [FIX] `UpdateTaskFullAppend`에서 이메일·room 소유권을 확인하고, 제목 변경과 기존 번역 삭제를 같은 transaction으로 처리. 같은 제목의 append는 번역을 보존.
- [FIX] `mc-util release-notes`가 Gemini 초기화/API 오류를 반환하며, 성공 전에는 릴리즈 노트나 백업 파일을 쓰지 않도록 처리.
- [TEST] 전체 `go test -race ./...`, 대상 패키지 `go vet`, Go build 통과. 초기화/API 실패의 파일 보존 및 번역 삭제 실패의 롤백을 검증.

---

# 업데이트 소식 (기술) - v2.4.8 (2026-10-27 UTC)

- [FIX] `GetMessageByID` 및 활성 메시지 cache 쿼리/sqlc mapping에 `deadline_date`, `deadline_inferred`를 전달해 캐시 유무와 관계없이 상세 조회의 명시·추론 마감일을 보존.
- [FIX] 번역 요청에 선택적 source title snapshot을 전달하고, `(tenant, language, message ID, Text)` 기준으로 singleflight 처리. 조회 행의 tenant 소유권을 확인하고, 제목 불일치 시 cache/AI 요청에서 제외하며 conditional 저장은 현재 제목과 일치한 ID만 반환. JIT도 snapshot 기준으로 실행.
- [FIX] 프런트엔드 번역 pending identity를 `(id, lang, sourceTitle)`로 바꾸고 batch `source_titles` payload를 전달. 실행 항목 identity와 일치하는 요청만 정리하고 현재 제목과 다른 응답 적용을 차단.
- [TEST] 프런트엔드 38개 파일·576개 테스트 통과, TypeScript 검사와 build 통과. 명시·추론·NULL 마감일 및 HTTP/JIT 번역 경합·소유권 회귀 검증 완료. `go test -race ./...` 전체 통과, Go build 통과.

---

# 업데이트 소식 (기술) - v2.4.7 (2026-10-27 UTC)

- [REFACTOR] 추출 guard와 task builder를 `GuardAndBuild` 경로로 통합하고, guarded item과 메시지를 함께 반환해 caller의 라우팅 정책을 보존. incoming ID를 유지하고 completion fallback 담당자 입력은 별도로 전달.
- [FIX] 번역 캐시의 단일 기준을 `task_translations` DB로 두고, 제목 변경과 기존 번역 삭제를 같은 transaction에서 처리.
- [FIX] 검색 상태와 request generation을 통합해 언어·검색어 변경 및 최신 응답 적용을 안정화.
- [FIX] 하위 작업의 로컬 optimistic 변경을 pending 상태로 추적하고 저장 실패 시 롤백.
- [CLEANUP] 사용되지 않는 메서드 제거.
- [TEST] 회귀 및 race fixture 검증 완료. `go test -race ./...` 전체 통과(services 78.090초); npm 37개 파일·569개 테스트 통과; TypeScript 검사와 build 통과.

---

# 업데이트 소식 (기술) - v2.4.6 (2026-04-19 15:33 UTC)



---

# 업데이트 소식 (기술) - v2.4.5 (2026-04-17 06:32 UTC)

- [STABILITY] 테스트 동시성 문제 해결을 위해 파일 기반 DB를 고유 인메모리 SQLite 연결로 교체
- [OPTIMIZE] Turso 및 SQLite의 초기화, 커넥션 풀링 및 스키마 마이그레이션 로직 최적화
- [FIX] 빈 메타데이터 처리 로직 추가를 통한 JSON 마샬링 오류 방지
- [FIX] 아카이브 로직 및 메시지 중복 제거 프로세스 수정

---

# 업데이트 소식 (기술) - v2.4.4 (2026-04-17 04:28 UTC)

- [REFACTOR] 메시지 캐시 및 아카이브 쿼리의 시간 기반 필터링 제거
- [REFACTOR] 메시지 처리 상태 추적 기능을 scan_metadata 테이블로 이관
- [FIX] ExtractJSONBlock 정규식의 공백 제거 로직 수정으로 데이터 손실 방지
- [FEAT] 필수 헤더를 포함한 보고서 구조 표준화 및 시각화 데이터 추출 로직 개선
- [FEAT] WhatsApp PushName 기반 연락처 매핑 구현 및 레거시 별칭 기능 대체
- [FIX] 별칭 동기화 기능을 포함한 백엔드 주도 작업 분류 시스템 구현
- [FEAT] 사용자 별칭을 통한 담당자 식별 강화 및 보고서 요약에 소스 컨텍스트 추가

---

# 업데이트 소식 (기술) - v2.4.3 (2026-04-17 02:02 UTC)

- [FEAT] AI 모델을 gemini-3-flash-preview로 업그레이드 및 리포트 제한 용량 16,000자로 확장
- [FEAT] Jaro-Winkler 유사도 기반 작업 중복 제거 및 동시 메시지 처리 방지 로직 구현
- [FEAT] 리포트 생성 시 채널/상태 필터 추가, i18n 다국어 지원 및 별칭 기반 담당자 식별 강화
- [UI] UI 펄스 애니메이션 도입 및 리포트 렌더링 로직 고도화
- [REFACTOR] 사용자, 연락처, 메시지 DB 작업을 단일화된 Upsert/Update 인터페이스로 통합
- [REFACTOR] DB 에러 로깅 중앙화 및 캐시 조회 입력 검증 헬퍼 도입
- [SYS] 작업 보관 로직 영향 수 로깅 추가 및 추출 파이프라인 조기 반환 최적화

---

# 업데이트 소식 (기술) - v2.4.2 (2026-04-15 02:30 UTC)

- [STABILITY] 데이터베이스 연결 안정성 강화 및 상세 에러 로깅 체계 구축
- [REFACTOR] 계층적 태스크 추출 로직 개편 및 서브태스크 마이그레이션 처리
- [FEAT] AI 기반 비동기 태스크 상태 전환 및 자동 완료 프로세스 도입
- [FEAT] 상태 폴링 방식의 비동기 리포트 생성 기능 및 문서화 지원

---

# 업데이트 소식 (기술) - v2.4.1 (2026-04-14 05:27 UTC)

- [REFACTOR] 핵심 로직 간소화 및 DB 스키마 정리를 위해 게임화 및 업적 관련 기능 제거
- [FEAT] Gmail 처리를 위한 AI 기반 노이즈 필터링 및 마케팅 메일 감지 기능 도입
- [FEAT] 레이스 컨디션 방지 및 가시성 개선을 위한 룸(Room) 단위 잠금 서비스 구현
- [REFACTOR] Slack 및 WhatsApp 스캐너 간 메시지 분류 및 담당자 정규화 로직 표준화
- [REFACTOR] 시스템 안정성 향상을 위해 API 에러 처리 및 컨텍스트 취소 로직 중앙 집중화
- [FEAT] 정교한 작업 해결을 위한 텍스트 유사도 서비스 및 시스템 프롬프트 개선
- [SYS] 비동기 작업의 안정적인 종료를 위해 스캐너 파이프라인에 WaitGroup 도입
- [PERF] 인메모리 테스트 인프라 전환 및 상태 동기화 최적화
- [FEAT] 토큰 사용량 통계에 노이즈 필터링 차단 수치 표시 기능 추가

---

