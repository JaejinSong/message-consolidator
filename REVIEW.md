# 구조 리뷰 및 후속 검증 기록

- 검토일: 2026-10-28 (KST)
- 기준 커밋: `d038b08b68d15221f66f1fa048e09a36c0ff4e8a`
- 상태: **초기 정적 리뷰 완료 / 상세 검증 및 구현 미착수**
- 목적: 이후 상세 리뷰에서 코드 근거, 미확인 사항, 최소 개선 범위를 이어서 확인합니다.

## 검토 방법과 한계

GPT-6 Astra / medium 서브에이전트 2개가 부모의 개선 가설이나 서로의 결과를 전달받지 않고 독립적으로 현재 구조를 검토했습니다. 한 에이전트는 아키텍처 관점으로, 다른 에이전트는 코드 리뷰 관점으로 조사했습니다. 두 결과를 취합한 뒤 subtask 트랜잭션 적용 여부만 추가 확인했습니다.

Serena 심볼·참조와 실제 호출 경로를 근거로 검토했습니다. 리뷰 과정에서 코드 변경, 테스트 실행, 실 DB 조회, 원격 API 호출은 수행하지 않았습니다. 아래 동시성·종료·화면 문제는 운영 장애나 브라우저 증상을 재현했다는 의미가 아닙니다. 심각도는 정적 분석상 위험을 기준으로 한 초기 판단입니다.

코드 위치는 위 기준 커밋의 경로와 1-based 줄번호입니다. 후속 변경으로 줄번호가 달라질 수 있으므로 함수 이름과 호출 경로를 함께 확인합니다. 이 문서는 기존 구조 문서를 대체하지 않으며, 현재 구조의 진입점은 [종합 문서 인덱스](docs/comprehensive/INDEX.md)입니다.

## 핵심 판단

파일 분할이나 새 추상화보다 **트랜잭션, 완료 전이, 캐시 공개, 종료 절차의 책임을 명확히 하는 작업**을 먼저 권고합니다. 두 독립 리뷰는 완료 규칙 분산과 AI 서비스 조립 중복을 공통으로 지적했습니다.

| ID | 초기 우선순위 | 항목 | 독립 리뷰 결과 | 상태 |
| --- | --- | --- | --- | --- |
| R1 | 높음 | subtask 변경의 트랜잭션 경계 | 코드 리뷰 발견 후 호출 인수 재확인 | Open |
| R2 | 높음 | commit과 캐시 무효화·갱신의 순서 | 코드 리뷰 발견 | Open |
| R3 | 높음 | 종료 절차의 소유권과 작업 대기 | 아키텍처 리뷰 발견 | Open |
| R4 | 중간 | 완료 규칙과 화면 반영 경로 분산 | 두 리뷰 공통 발견 | Open |
| R5 | 중간 | AI 서비스 중복 조립과 초기화 실패 범위 | 두 리뷰 공통 발견 | Open |
| R6 | 중간 | 프런트 완료 명령과 렌더링 중복 | 아키텍처 리뷰 발견 | Open |

## R1. Subtask 변경의 트랜잭션 경계

### 확인한 사실

- [handlers/handlers_msgs.go](handlers/handlers_msgs.go):215에서 `UpdateSubtaskStatus`에 `store.GetDB()`를 전달합니다.
- [store/db.go](store/db.go):296의 `GetDB()` 반환형은 `*sql.DB`입니다.
- [store/message_update.go](store/message_update.go):269에서 전달된 인수를 `withTx`에 그대로 넘깁니다.
- [store/message_save.go](store/message_save.go):17–22의 `withTx`는 인수가 `nil`일 때만 새 트랜잭션을 시작합니다. `*sql.DB`와 `*sql.Tx`를 구분하지 않습니다.
- 따라서 실제 HTTP 경로에서는 메시지 조회, subtask JSON 변경, 부모 완료를 하나의 트랜잭션으로 묶지 않습니다. `nil`이면 새 트랜잭션, `*sql.Tx`이면 기존 트랜잭션 재사용, `*sql.DB`이면 개별 SQL 실행입니다.
- 반면 [services/task_routing.go](services/task_routing.go):22의 `runTaskTx`는 `*sql.Tx` 여부를 기준으로 트랜잭션을 시작하여 helper 간 의미가 다릅니다.

초기 아키텍처 리뷰의 “현재 UpdateSubtaskStatus가 트랜잭션을 사용한다”는 설명은 helper 호출만으로 판단한 내용입니다. 실제 handler 인수까지 재확인한 위 결론으로 정정합니다. 소스의 “within a transaction” 주석만으로 원자성을 판단하지 않습니다.

### 위험과 최소 개선안

동시에 서로 다른 subtask를 변경하면 같은 JSON을 읽고 마지막 쓰기가 앞선 변경을 덮어쓸 가능성이 있습니다. 부모 완료 실패를 로그만 남기고 성공으로 반환하는 정책도 함께 검토해야 합니다.

서비스가 유스케이스의 트랜잭션을 소유하고, Store가 기존 `*sql.Tx`를 사용하도록 정리합니다. helper 전체를 일괄 교체하기보다 이 HTTP 경로부터 좁게 적용합니다. 트랜잭션 도입만으로 모든 동시성 문제가 해결된다고 가정하지 않고 SQLite/libsql의 충돌·재시도 동작까지 검증합니다.

### 후속 검증

- [ ] 서로 다른 subtask를 동시에 변경하여 갱신 유실 여부를 확인합니다.
- [ ] 부모 완료 SQL 실패 시 부분 저장 여부와 API 반환값을 확인합니다.
- [ ] `nil`, `*sql.DB`, `*sql.Tx` 호출자의 의도를 구분합니다.
- [ ] 로컬 SQLite와 원격 libsql에서 충돌 처리·재시도 정책을 확인합니다.

변경 비용은 중간입니다. 범용 UnitOfWork 프레임워크 도입은 현재 권고하지 않습니다.

## R2. Commit과 캐시 무효화·갱신의 순서

### 확인한 사실

AI 완료 경로는 `handleResolve → runTaskTx → MarkMessageDone`입니다. [store/message_update.go](store/message_update.go):177의 캐시 무효화는 트랜잭션 callback 안에서 발생하며, 실제 commit은 callback 반환 뒤에 실행됩니다. 관련 경로는 [services/task_routing.go](services/task_routing.go):369와 [store/db.go](store/db.go):326입니다.

[store/cache_store.go](store/cache_store.go):172의 `RefreshCache`는 DB 조회 이후 잠금을 잡고 결과를 게시합니다. 조회와 게시 사이의 무효화를 이전 조회 결과가 덮어쓸 수 있습니다. 유효 캐시는 만료 시간 없이 재사용됩니다.

### 위험과 최소 개선안

commit 이전 조회가 이전 DB 상태를 읽어 캐시를 다시 채우거나, 진행 중이던 refresh가 새 무효화를 덮어쓸 가능성이 있습니다.

무효화 책임을 최상위 commit 성공 이후로 옮깁니다. refresh 시작 이후 해당 사용자의 무효화가 발생했는지 generation 등으로 확인하여 오래된 결과를 게시하지 않습니다. 기존 `MergeTasksWithTitle`의 commit 후 무효화 패턴을 비교 기준으로 사용할 수 있습니다.

### 후속 검증

- [ ] commit 직전 조회를 끼워 넣어 이전 결과의 캐시 재등록을 재현합니다.
- [ ] refresh의 조회·게시 사이에 무효화를 발생시키는 결정적 테스트를 작성합니다.
- [ ] rollback과 commit 실패 시 캐시 처리 정책을 확인합니다.
- [ ] 사용자별 캐시 경계와 모든 갱신 경로를 확인합니다.

변경 비용은 중간입니다. Redis나 별도 캐시 서비스 도입은 필요하지 않습니다.

## R3. 종료 절차의 소유권과 작업 대기

### 확인한 사실

[main.go](main.go):96, 178, 241과 [scanner/scanner.go](scanner/scanner.go):93에서 종료 책임이 나뉩니다. main은 취소 후 shutdown을 실행하지만 scanner 내부 WaitGroup 대기의 완료를 기다리지 않습니다. scanner는 별도 goroutine에서 최대 30초 대기합니다.

shutdown에서는 외부 클라이언트 연결 해제와 메모리 flush가 HTTP 요청 drain보다 먼저 진행됩니다. scanner 대기 중 DB 종료가 진행되거나, 진행 요청·작업이 최종 flush 이후 데이터를 변경할 여지가 있습니다. 실제 유실은 확인하지 않았습니다.

### 최소 개선안과 유지할 장점

scanner 완료 채널 또는 명시적인 `Wait`를 상위 호출자에 제공합니다. 한 종료 함수가 HTTP 유입 중단·진행 요청 drain·백그라운드 취소 및 대기·최종 flush·DB close의 의존 순서를 조율합니다. 요청이 외부 클라이언트를 사용하므로 실제 사용 관계를 확인하여 disconnect 위치를 결정합니다.

현재 scanner의 자체 대기 구현과 독립 종료 작업의 병렬 실행은 재사용할 가치가 있습니다. 전체 종료 예산 안에서 완료를 관찰하도록 연결하며 lifecycle 프레임워크는 추가하지 않습니다.

### 후속 검증

- [ ] 진행 중 HTTP 요청과 scanner 작업이 있는 상태에서 종료 순서를 기록합니다.
- [ ] 최종 flush 이후 쓰기 및 DB close 이후 접근이 없는지 확인합니다.
- [ ] 전체 종료 시간 제한과 강제 종료 정책을 확인합니다.
- [ ] 외부 클라이언트 disconnect와 작업 취소 간 의존성을 확인합니다.

변경 비용은 중간입니다.

## R4. 완료 규칙과 화면 반영 경로 분산

### 확인한 사실

일반 완료는 Handler → TasksService → Store를 거치지만, subtask 변경은 Handler → Store로 직행합니다. 마지막 subtask 완료에 따른 부모 자동 완료 판단도 Store에 있습니다. 일반 완료 이후 학습 기록은 Handler에서 별도 실행합니다.

근거는 [handlers/handlers_msgs.go](handlers/handlers_msgs.go):98, 124, 199, [services/tasks.go](services/tasks.go):182, [store/message_update.go](store/message_update.go):268, 329입니다.

프런트 subtask 처리는 subtask의 `done`을 변경하고 서버 응답을 사용하지 않습니다. 서버에서 부모가 자동 완료되어도 부모 상태·통계는 다음 조회까지 이전 값일 수 있습니다. 관련 경로는 [src/app.ts](src/app.ts):243과 [src/state.ts](src/state.ts):163입니다. 실제 화면 재현은 미수행입니다.

### 최소 개선안과 정책 질문

`TasksService`에 subtask 변경 유스케이스를 추가하여 부모 완료 판단, 원자성, 실패 정책, 후속 처리를 함께 관리합니다. API는 변경된 부모 상태를 반환하고 화면은 이를 반영합니다. R1·R2와 같은 변경 경로이므로 함께 설계하되 검증 항목은 분리합니다.

자동 완료를 수동 완료와 동일한 학습 신호로 취급할지는 제품 정책 확인이 필요합니다. 현재 차이를 학습 버그로 단정하지 않습니다. Store 호출만으로 규칙을 빠뜨리지 않던 장점을 서비스 진입점에서도 보존해야 합니다.

### 후속 검증

- [ ] 직접 완료, 마지막 subtask 완료, 완료 해제, AI 완료의 전이 표를 작성합니다.
- [ ] 경로별 학습·통계·캐시·API 응답 차이를 확인합니다.
- [ ] 마지막 subtask 완료 직후 부모 카드와 통계가 일치하는지 검증합니다.
- [ ] 규칙 이동 이후 트랜잭션 원자성이 유지되는지 확인합니다.

변경 비용은 중간입니다. 읽기 전용 호출까지 서비스 wrapper로 일괄 전환하거나 이벤트 버스·CQRS를 도입하지 않습니다.

## R5. AI 서비스 중복 조립과 초기화 실패 범위

### 확인한 사실

[main.go](main.go):75, 149와 [scanner/scanner_wiring.go](scanner/scanner_wiring.go):13에서 AI client, TranslationService, TasksService를 각각 생성합니다. ProviderConfig 설정도 중복됩니다. scanner는 전역 의존성을 설정한 뒤 별도 wiring 함수로 추가 필드를 채웁니다.

[services/translation_service.go](services/translation_service.go):17의 singleflight와 semaphore는 인스턴스별입니다. HTTP와 scanner 사이에는 중복 요청 합치기·동시성 한도가 공유되지 않습니다. 실제 중복 요청량이나 rate limit 발생은 측정하지 않았습니다.

scanner의 AI 초기화 실패는 뒤에 있는 Slack client, reminder, exclusion, past-event 초기화도 건너뜁니다. `Init`은 오류를 반환하지 않고, `WireDailyDigest`는 Slack client가 없으면 비활성화됩니다. 관련 위치는 [scanner/scanner_wiring.go](scanner/scanner_wiring.go):69입니다.

### 최소 개선안과 유지할 장점

먼저 AI 실패가 독립 기능 초기화까지 중단하지 않도록 하고 초기화 결과를 명시합니다. 이후 main의 조립 함수에서 의존성을 생성해 scanner에 전달합니다.

현재 인스턴스 분리는 사용자 요청과 백그라운드 작업의 동시성 한도를 격리합니다. 이 격리가 의도라면 공통 factory로 설정 중복만 제거하고 인스턴스 분리는 유지할 수 있습니다. 공통 인스턴스가 항상 더 낫다고 가정하지 않습니다.

### 후속 검증

- [ ] AI client 생성 실패 시 각 채널·digest·reminder의 활성 상태를 확인합니다.
- [ ] 두 인스턴스가 동일 설정과 다른 설정을 사용하는 부분을 구분합니다.
- [ ] 공유 시 사용자 요청 지연과 백그라운드 작업의 경합을 평가합니다.
- [ ] 부분 초기화·종료 시 자원 소유권을 명확히 합니다.

실패 범위 수정 비용은 낮음, 조립 책임 통합 비용은 중간입니다. DI 컨테이너나 scanner 전면 재작성은 권고하지 않습니다.

## R6. 프런트 완료 명령과 렌더링 중복

### 확인한 사실

[src/app.ts](src/app.ts):63, 129의 일반 완료와 완료 후보 확인은 API 호출, 낙관적 상태 변경, DOM 갱신, 실패 복구를 각각 구현합니다.

상태 변경은 [src/state.ts](src/state.ts):153, 부분 DOM 갱신은 [src/renderer.ts](src/renderer.ts):411, 전체 카드 규칙은 [src/components/message-card.ts](src/components/message-card.ts):108에 나뉩니다. 부분 갱신과 전체 카드 생성이 다루는 완료 관련 표시가 달라 같은 상태에서 동일 화면을 보장하기 어렵습니다.

### 최소 개선안과 유지할 장점

`setTaskDone(id, done)` 같은 하나의 명령으로 두 동작을 연결하고, 기존 [src/renderer.ts](src/renderer.ts):279의 `updateMessageCard` 재사용을 검토합니다. 실패 시 반대 값으로 추정하지 않고 이전 상태 snapshot으로 복구합니다. 동일 업무의 중복 변경도 이 경로에서 관리합니다.

현재 부분 갱신은 focus, 편집 중 입력, 펼친 details를 보존하는 장점이 있습니다. 카드 교체가 이 상태를 잃지 않는지 먼저 확인합니다. 프레임워크 전환이나 범용 상태관리 계층 도입은 필요하지 않습니다.

### 후속 검증

- [ ] 두 완료 동작의 성공·실패·중복 클릭 흐름을 비교합니다.
- [ ] 부분 갱신과 새 카드 생성 결과를 완료 배너·마감·정체 표시까지 비교합니다.
- [ ] 서버 응답 역전과 낙관적 변경 rollback을 검증합니다.
- [ ] 카드 교체 시 focus·입력·펼침 상태 보존을 확인합니다.

변경 비용은 낮음~중간입니다.

## 다음 상세 리뷰 진행 순서

1. R1·R2의 실패 시나리오를 결정적으로 재현하고 실제 영향 범위를 확정합니다.
2. R4의 완료 전이 표와 API 반환 계약을 정리합니다. R1·R2를 포함한 최소 변경 범위를 설계합니다.
3. R3의 종료 의존 관계와 시간 제한을 검증합니다.
4. R5의 초기화 실패 정책과 인스턴스 격리 의도를 확인합니다.
5. R6의 기존 renderer 재사용 가능성과 상호작용 상태 보존을 검증합니다.

후속 기록에는 각 ID별 재현 결과, 판단 변경, 관련 테스트, 구현 커밋을 남깁니다. 위 체크박스는 아직 수행하지 않은 검증이며, 이 문서 작성으로 개선 작업이 완료된 것은 아닙니다.
