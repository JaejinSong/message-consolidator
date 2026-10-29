# 구조 리뷰 및 후속 검증 기록

- 검토일: 2026-10-28 (KST)
- 기준 커밋: `d038b08b68d15221f66f1fa048e09a36c0ff4e8a`
- 상태: **초기 정적 리뷰 완료 / 교차검토 반영 / R1·R2 일부 구현(S1·S2) / R3 부분 구현 / 나머지 미착수**
- 교차검토: 2026-10-29 (정적 코드 대조, 기준 HEAD `b940e4f`. 이 문서 외 코드는 `d038b08`과 동일)
- 목적: 이후 상세 리뷰에서 코드 근거, 미확인 사항, 최소 개선 범위를 이어서 확인합니다.

## 검토 방법과 한계

GPT-6 Astra / medium 서브에이전트 2개가 부모의 개선 가설이나 서로의 결과를 전달받지 않고 독립적으로 현재 구조를 검토했습니다. 한 에이전트는 아키텍처 관점으로, 다른 에이전트는 코드 리뷰 관점으로 조사했습니다. 두 결과를 취합한 뒤 subtask 트랜잭션 적용 여부만 추가 확인했습니다.

Serena 심볼·참조와 실제 호출 경로를 근거로 검토했습니다. 리뷰 과정에서 코드 변경, 테스트 실행, 실 DB 조회, 원격 API 호출은 수행하지 않았습니다. 아래 동시성·종료·화면 문제는 운영 장애나 브라우저 증상을 재현했다는 의미가 아닙니다. 심각도는 정적 분석상 위험을 기준으로 한 초기 판단입니다.

2026-10-29 교차검토에서는 서브에이전트 3개가 이 문서를 신뢰하지 않고 R1~R6의 각 주장을 코드와 다시 대조했습니다. 핵심 진단은 모두 코드와 일치했으며, 정정·누락 사항은 각 항목의 “교차검토 정정” 절에 기록합니다. 교차검토도 정적 읽기이며 테스트 실행, 실 DB 조회, 브라우저 재현은 수행하지 않았습니다.

코드 위치는 위 기준 커밋의 경로와 1-based 줄번호입니다. 후속 변경으로 줄번호가 달라질 수 있으므로 함수 이름과 호출 경로를 함께 확인합니다. 이 문서는 기존 구조 문서를 대체하지 않으며, 현재 구조의 진입점은 [종합 문서 인덱스](docs/comprehensive/INDEX.md)입니다.

## 핵심 판단

파일 분할이나 새 추상화보다 **트랜잭션, 완료 전이, 캐시 공개, 종료 절차의 책임을 명확히 하는 작업**을 먼저 권고합니다. 두 독립 리뷰는 완료 규칙 분산과 AI 서비스 조립 중복을 공통으로 지적했습니다.

| ID | 초기 우선순위 | 항목 | 독립 리뷰 결과 | 상태 |
| --- | --- | --- | --- | --- |
| R1 | 높음 | subtask 변경의 트랜잭션 경계 | 코드 리뷰 발견 후 호출 인수 재확인 | 구현됨(원격 libsql 검증 대기) |
| R2 | 높음 | commit과 캐시 무효화·갱신의 순서 | 코드 리뷰 발견 | 부분 구현(S5 보류) |
| R3 | 높음 | 종료 절차의 소유권과 작업 대기 | 아키텍처 리뷰 발견 | 부분 구현(잔여 Open은 구현 기록 참조) |
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

### 교차검토 정정 (2026-10-29)

판정: 확인. 핵심 주장은 코드와 일치합니다.

- 운영 코드에서 `withTx`에 `nil`을 넘기는 호출자는 없습니다. 호출자는 `GetDB()`뿐이며([handlers/handlers_msgs.go](handlers/handlers_msgs.go):215, [services/tasks.go](services/tasks.go):191, [services/task_routing.go](services/task_routing.go)) `nil` 분기는 현재 사용되지 않습니다.
- 부모 완료(`MarkMessageDone`)는 단일 UPDATE이므로 그 자체의 원자성은 문제가 아닙니다. 실제 위험은 subtask JSON 전체를 다시 쓰는 read-modify-write입니다(`UpdateSubtasks`, [store/queries/messages.sql](store/queries/messages.sql):46). 조회([store/message_update.go](store/message_update.go):288 부근), 쓰기(:315 부근), 부모 완료(:330)가 각각 autocommit입니다.
- 이 경로를 직렬화하는 장치는 없습니다. 연결 풀 상한은 `file:` DSN 100, libsql 25이며 `SetMaxOpenConns(1)`은 testMode에서만 적용됩니다([store/db.go](store/db.go):245-280).
- 부모 완료 실패는 `Warnf` 후 `nil`을 반환합니다([store/message_update.go](store/message_update.go):330-336). 문서의 정책 질문이 유효합니다.
- 누락한 대안: `json_set`으로 해당 subtask 원소만 갱신하는 단일 문장은 read-modify-write 자체를 없앱니다. 트랜잭션 도입과 비교하여 검토합니다.
- `RunInTx`는 WAL SQLite의 `SQLITE_BUSY_SNAPSHOT`을 `isLockError`로 재시도하므로 로컬 적용은 가능합니다. 원격 libsql HTTP의 격리 동작은 여전히 미확인입니다.

### 위험과 최소 개선안

동시에 서로 다른 subtask를 변경하면 같은 JSON을 읽고 마지막 쓰기가 앞선 변경을 덮어쓸 가능성이 있습니다. 부모 완료 실패를 로그만 남기고 성공으로 반환하는 정책도 함께 검토해야 합니다.

서비스가 유스케이스의 트랜잭션을 소유하고, Store가 기존 `*sql.Tx`를 사용하도록 정리합니다. helper 전체를 일괄 교체하기보다 이 HTTP 경로부터 좁게 적용합니다. 트랜잭션 도입만으로 모든 동시성 문제가 해결된다고 가정하지 않고 SQLite/libsql의 충돌·재시도 동작까지 검증합니다.

### 후속 검증

- [x] 서로 다른 subtask를 동시에 변경하여 갱신 유실 여부를 확인합니다. (변경 전 첫 반복에서 유실 재현, 변경 후 file DSN 50회 반복 통과)
- [ ] 부모 완료 SQL 실패 시 부분 저장 여부와 API 반환값을 확인합니다.
- [ ] `nil`, `*sql.DB`, `*sql.Tx` 호출자의 의도를 구분합니다.
- [x] 로컬 SQLite와 libsql(sqld)에서 S2 문장의 동작을 확인합니다. (충돌·재시도 정책은 단일 문장이라 해당 없음, 운영 Turso 쓰기는 미실행)

변경 비용은 중간입니다. 범용 UnitOfWork 프레임워크 도입은 현재 권고하지 않습니다.

### 구현 기록

- 구현 커밋: `790924e`, `4028428`. 트랜잭션 대신 `json_set` 단일 문장과 조건부 부모 완료 문장으로 교체하여 read-modify-write를 제거했습니다(비테스트 코드 순 -19줄). 근거와 비교는 R4의 분할·우선순위 제안을 따릅니다.
- 동작 변화: 이미 완료된 부모에서 마지막 subtask를 다시 체크해도 `completed_at`을 갱신하지 않습니다. 없는 행·타 사용자·범위 밖 인덱스는 하나의 오류로 합쳐졌습니다.
- libsql 검증(2026-10-29): (1) 원격 Turso(libsql 3.47.0)에서 SELECT 전용으로 `json_set`+`json('true')`, `json_each`+`IS NOT 1`, `BETWEEN` 경계 식을 확인했습니다(쓰기 없음). (2) 로컬 `sqld`(HTTP, 실제 libsql 드라이버)에서 [store/subtask_libsql_test.go](store/subtask_libsql_test.go)의 5개 시나리오(순차 완료·부분·범위 밖/타 사용자·완료 후 해제·동시 토글 20회)가 통과했습니다. 테스트는 `LIBSQL_TEST_URL` 설정 시에만 실행됩니다. 드라이버는 Go bool을 정수 1/0으로 바인딩하며 `CASE WHEN ?`와 호환됩니다.
- 미검증: 운영 Turso(원격)에 대한 쓰기 경로 실행과 지연 측정은 하지 않았습니다. sqld는 단일 서버라 원격 HTTP 트랜잭션 격리 차이는 재현하지 못합니다. 부모 완료 실패 정책은 현재 `Warnf` 후 성공 반환을 유지하며 R4 정책 (a)의 결정을 기다립니다.

## R2. Commit과 캐시 무효화·갱신의 순서

### 확인한 사실

AI 완료 경로는 `handleResolve → runTaskTx → MarkMessageDone`입니다. [store/message_update.go](store/message_update.go):177의 캐시 무효화는 트랜잭션 callback 안에서 발생하며, 실제 commit은 callback 반환 뒤에 실행됩니다. 관련 경로는 [services/task_routing.go](services/task_routing.go):369와 [store/db.go](store/db.go):326입니다.

[store/cache_store.go](store/cache_store.go):172의 `RefreshCache`는 DB 조회 이후 잠금을 잡고 결과를 게시합니다. 조회와 게시 사이의 무효화를 이전 조회 결과가 덮어쓸 수 있습니다. 유효 캐시는 만료 시간 없이 재사용됩니다.

### 교차검토 정정 (2026-10-29)

판정: 부분 확인. 아래 정정 외 주장은 일치합니다.

- 줄번호 정정: 무효화는 `markMessageDoneTrue`의 `InvalidateCache`([store/message_update.go](store/message_update.go):188), commit은 `runTxOnce`의 `tx.Commit()`([store/db.go](store/db.go):337)입니다.
- “commit 전 무효화”는 호출자별로 다릅니다. `handleResolve`와 `RunInTx` 계열은 commit 전입니다. `HandleTaskCompletion`은 `GetDB()` autocommit이라 문장 완료 직후, 즉 commit 후입니다([services/tasks.go](services/tasks.go):191). 전체 `InvalidateCache` 호출처는 감사하지 않았습니다.
- `RunInTx`는 callback을 최대 3회 재시도하므로([store/db.go](store/db.go):308-324) 롤백되는 시도에서도 무효화가 발생합니다.
- `RefreshArchiveCache`([store/cache_store.go](store/cache_store.go):199)도 `RefreshCache`와 같은 조회·게시 경합 결함을 가집니다.
- `updateSubtaskStatusInternal`은 `MarkMessageDone`의 무효화 뒤에 `InvalidateCacheActive`를 다시 호출합니다([store/message_update.go](store/message_update.go):335).
- 수정안 우선순위 정정: commit 이후로 무효화를 옮기는 것만으로는 부족합니다. commit 이전에 시작된 refresh가 무효화 뒤에 이전 결과를 게시할 수 있으므로 **사용자별 generation 검사(무효화 시 `cacheMu` 아래에서 증가, 게시 전 비교)가 핵심**이며 순서 이동은 보조입니다. 순서 이동에는 Store helper가 `Querier`만 받아 commit을 볼 수 없으므로 post-commit hook 또는 반환 플래그 설계가 필요합니다.

### 위험과 최소 개선안

commit 이전 조회가 이전 DB 상태를 읽어 캐시를 다시 채우거나, 진행 중이던 refresh가 새 무효화를 덮어쓸 가능성이 있습니다.

무효화 책임을 최상위 commit 성공 이후로 옮깁니다. refresh 시작 이후 해당 사용자의 무효화가 발생했는지 generation 등으로 확인하여 오래된 결과를 게시하지 않습니다. 기존 `MergeTasksWithTitle`의 commit 후 무효화 패턴을 비교 기준으로 사용할 수 있습니다.

### 후속 검증

- [ ] commit 직전 조회를 끼워 넣어 이전 결과의 캐시 재등록을 재현합니다.
- [x] refresh의 조회·게시 사이에 무효화를 발생시키는 결정적 테스트를 작성합니다. (`f6d10de`, 테스트 5개)
- [ ] rollback과 commit 실패 시 캐시 처리 정책을 확인합니다.
- [ ] 사용자별 캐시 경계와 모든 갱신 경로를 확인합니다.

변경 비용은 중간입니다. Redis나 별도 캐시 서비스 도입은 필요하지 않습니다.

### 구현 기록

- 구현 커밋: `f6d10de`. 사용자별 generation 카운터를 `InvalidateCache*`에서 증가시키고, `RefreshCache`·`RefreshArchiveCache`는 조회 전 값을 저장했다가 게시 직전에 비교하여 달라졌으면 게시하지 않습니다(비테스트 코드 +31/-2줄). active와 archive가 카운터를 공유하므로 한쪽 무효화가 다른 쪽 refresh 게시도 버리며, 다음 조회에서 DB를 다시 읽습니다.
- 운영 코드에 테스트용 `refreshPublishHook`이 남아 있습니다. refresh가 구체 `*sql.DB`를 직접 쓰므로 hook 없이는 조회·게시 사이의 경합을 결정적으로 재현할 수 없습니다.
- 미해결(S5, 보류): `handleResolve`/`RunInTx` 경로의 commit 전 무효화 창은 그대로이며, generation 검사로 stale 게시는 막지만 commit 이후 재무효화는 하지 않습니다. 롤백·commit 실패 정책과 사용자별 경계 확인도 남아 있습니다.

## R3. 종료 절차의 소유권과 작업 대기

### 확인한 사실

[main.go](main.go):96, 178, 241과 [scanner/scanner.go](scanner/scanner.go):93에서 종료 책임이 나뉩니다. main은 취소 후 shutdown을 실행하지만 scanner 내부 WaitGroup 대기의 완료를 기다리지 않습니다. scanner는 별도 goroutine에서 최대 30초 대기합니다.

shutdown에서는 외부 클라이언트 연결 해제와 메모리 flush가 HTTP 요청 drain보다 먼저 진행됩니다. scanner 대기 중 DB 종료가 진행되거나, 진행 요청·작업이 최종 flush 이후 데이터를 변경할 여지가 있습니다. 실제 유실은 확인하지 않았습니다.

### 교차검토 정정 (2026-10-29)

판정: 부분 확인. 소유권 분산과 scanner 미대기는 확인되었고 종료 순서 서술을 보완합니다.

- 실제 종료 순서([main.go](main.go):178-216)는 WhatsApp·Telegram 연결 해제와 token usage·scan metadata flush(병렬, `wg.Wait()` :201) → `srv.Shutdown`(30초 제한, :206) → `db.Close()`(:212)입니다. `srv.Shutdown`은 세 번째입니다.
- `StartBackgroundScanner`는 fire-and-forget이며([main.go](main.go):242) 완료 채널이나 `Wait`가 없습니다. main이 반환되면 프로세스가 종료되므로 scanner의 30초 대기([scanner/scanner.go](scanner/scanner.go):93-108)는 HTTP drain이 그 시간만큼 걸리지 않는 한 사실상 효과가 없습니다.
- 진행 중인 scanner 작업(AI 호출, `store.*` 쓰기)이 `db.Close()`와 겹칠 수 있고, flush 이후 token usage·scan metadata 쓰기가 발생해도 두 번째 flush가 없습니다. 실제 유실은 여전히 미확인입니다.
- 누락: `defer trace.Shutdown()`과 `defer cancel()`은 `gracefulShutdown` 이후에 실행됩니다. notion-wa logger와 `bootChannelClients` goroutine도 같은 ctx를 쓰지만 대기하지 않습니다.
- 진행 중 HTTP 요청이 이미 해제된 클라이언트를 호출할 수 있습니다(`channels.GetWhatsAppQR`, `LogoutWhatsApp`, `StartTelegramAuth`; [handlers/handlers_whatsapp.go](handlers/handlers_whatsapp.go):27/38, [handlers/handlers_telegram.go](handlers/handlers_telegram.go):106). disconnect 위치를 사용 관계로 정하라는 권고가 유효합니다.
- 수정안 순서 제안: `srv.Shutdown` → ctx 취소 후 scanner 대기 → 연결 해제·flush → `db.Close()`. scanner 대기와 HTTP drain이 각각 30초를 쓰지 않도록 전체 종료 예산을 공유해야 합니다.

### 최소 개선안과 유지할 장점

scanner 완료 채널 또는 명시적인 `Wait`를 상위 호출자에 제공합니다. 한 종료 함수가 HTTP 유입 중단·진행 요청 drain·백그라운드 취소 및 대기·최종 flush·DB close의 의존 순서를 조율합니다. 요청이 외부 클라이언트를 사용하므로 실제 사용 관계를 확인하여 disconnect 위치를 결정합니다.

현재 scanner의 자체 대기 구현과 독립 종료 작업의 병렬 실행은 재사용할 가치가 있습니다. 전체 종료 예산 안에서 완료를 관찰하도록 연결하며 lifecycle 프레임워크는 추가하지 않습니다.

### 후속 검증

- [ ] 진행 중 HTTP 요청과 scanner 작업이 있는 상태에서 종료 순서를 기록합니다.
- [ ] 최종 flush 이후 쓰기 및 DB close 이후 접근이 없는지 확인합니다.
- [x] 전체 종료 시간 제한과 강제 종료 정책을 확인합니다. (공유 예산 31초, drain 실패 시 `srv.Close()`)
- [ ] 외부 클라이언트 disconnect와 작업 취소 간 의존성을 확인합니다.

변경 비용은 중간입니다.

### 구현 기록

- 구현: `StartBackgroundScanner`가 비차단이 되어 모든 루프 종료 시 닫히는 `<-chan struct{}`를 반환하고, 내부 30초 타임아웃은 제거했습니다([scanner/scanner.go](scanner/scanner.go)). [main.go](main.go)의 `runShutdown`이 순서를 소유합니다: `cancel()` → HTTP drain(drain 실패 시 `srv.Close()`) → scanner 대기 → 연결 해제·flush(병렬) → `db.Close()`. drain과 scanner 대기는 하나의 예산(`shutdownBudget` 31초)을 공유하고, 예산이 소진돼도 마지막 두 단계는 항상 실행합니다.
- 순서 편차: 교차검토 제안은 `srv.Shutdown`을 먼저 두었으나, `RunAllScans(ctx)` 클로저처럼 메인 ctx를 쓰는 핸들러가 예산을 모두 소진하면 scanner 대기 시간이 0이 되므로(코드 리뷰 HIGH) `cancel()`을 drain 앞에 두었습니다. 이는 변경 전 동작과 같고 scanner 정리가 drain과 병행됩니다.
- 테스트: [main_shutdown_test.go](main_shutdown_test.go) 5개(순서, background 대기, 미종료 채널의 예산 만료, drain 오류, cancel 선행), [scanner/scanner_shutdown_test.go](scanner/scanner_shutdown_test.go) 3개. `-race` 통과.
- Open(미착수, 코드 리뷰 지적):
  - Slack·completion의 detached goroutine(`WithoutCancel` + 293초)이 scanner `wg` 밖이라 `done`에 포함되지 않습니다.
  - Telegram `DisconnectAll`이 `runClient` 종료를 기다리지 않고, WhatsApp 이벤트 핸들러(`dbLogger.Receive`, `UpdateUserWAJID`)가 `db.Close()` 이후에도 쓸 수 있습니다.
  - notion-wa logger의 최종 flush가 `db.Close()`와 경합하고, `HandleManualScan`의 untracked goroutine이 있습니다.
  - `scanner_shutdown_test.go`는 첫 tick이 panic으로 끝나도 통과하므로 "진행 중 루프를 기다린다"는 동작을 검증하지 못합니다. 루프 목록 주입 구조가 필요합니다.
  - 전체 종료 시간은 최악 약 31초 + WA 2초 + flush이며 `docker-compose.yml`의 `stop_grace_period: 1m` 안입니다. `flush`에는 타임아웃이 없습니다(기존과 동일).

## R4. 완료 규칙과 화면 반영 경로 분산

### 확인한 사실

일반 완료는 Handler → TasksService → Store를 거치지만, subtask 변경은 Handler → Store로 직행합니다. 마지막 subtask 완료에 따른 부모 자동 완료 판단도 Store에 있습니다. 일반 완료 이후 학습 기록은 Handler에서 별도 실행합니다.

근거는 [handlers/handlers_msgs.go](handlers/handlers_msgs.go):98, 124, 199, [services/tasks.go](services/tasks.go):182, [store/message_update.go](store/message_update.go):268, 329입니다.

프런트 subtask 처리는 subtask의 `done`을 변경하고 서버 응답을 사용하지 않습니다. 서버에서 부모가 자동 완료되어도 부모 상태·통계는 다음 조회까지 이전 값일 수 있습니다. 관련 경로는 [src/app.ts](src/app.ts):243과 [src/state.ts](src/state.ts):163입니다. 실제 화면 재현은 미수행입니다.

### 교차검토 정정 (2026-10-29)

판정: 부분 확인. 백엔드 주장은 확인되었고 프런트 주장을 정정합니다.

- 확인: `HandleMarkDone`은 `Tasks.HandleTaskCompletion`을 거치고, `HandleToggleSubtask`([handlers/handlers_msgs.go](handlers/handlers_msgs.go):199, 호출 :215)는 `a.Tasks`를 거치지 않고 `store.UpdateSubtaskStatus`를 직접 호출합니다. 부모 자동 완료는 `updateSubtaskStatusInternal`([store/message_update.go](store/message_update.go):329-332)에 있습니다. 학습 기록(`recordUneditedCompletionIfLoaded`, :124/:512)은 `HandleMarkDone`에만 있습니다.
- 정정: “부모 통계가 다음 조회까지 이전 값”은 근거가 없습니다. `updateStats`([src/state.ts](src/state.ts):55)는 `archive_days`와 `stale_threshold_working_days`만 읽으며 task 수치에서 파생되지 않습니다. 실제로 이전 값이 남는 것은 부모 카드(done 클래스·버튼 아이콘·배너), 사이드바 active 카운트, “(n/m)” 진행 라벨(`updateSubtaskNodeStatus`, [src/renderer.ts](src/renderer.ts):494는 항목 클래스와 체크 표시만 갱신)입니다. subtask 경로는 `TASK_COMPLETED` 이벤트도 발생시키지 않습니다.
- 정정: subtask API 응답은 `{user}`뿐이며(`respondWithUpdatedUser`, [handlers/handlers_msgs.go](handlers/handlers_msgs.go):320) 부모 상태를 담지 않습니다. “부모 상태 반환”은 기존 계약이 아니라 신규 제안입니다.
- 누락: 프런트 subtask 처리는 이미 중복 클릭 가드(`pendingSubtaskUpdates`)와 롤백 시 식별 검사를 갖습니다. `onToggleDone`에는 둘 다 없습니다.
- 수정안은 타당합니다. `TasksService` 유스케이스는 단일 트랜잭션을 유지하고, 응답에 부모의 `done`과 `completed_at`(또는 메시지)을 반환하며, 학습 신호는 정책 결정으로 남깁니다.

### 분할·우선순위 제안 (architect 리뷰, 2026-10-29)

설계 제안이며 SQL 동작은 아직 실행으로 검증하지 않았습니다. R4를 독립 배포 가능한 4단계와 보류 1단계로 나눕니다.

**방식 선택: 트랜잭션 대신 `json_set` 단일 문장.** 서비스 트랜잭션으로 원자성을 확보하는 안은 Store helper를 `*sql.Tx` 기준으로 바꾸는 배관을 되살립니다. 아래 두 문장은 각각 단독으로 원자적이고 재실행해도 같은 상태가 됩니다.

1. `json_set`으로 해당 subtask의 `done`만 갱신합니다. `:execrows`가 0이면 없는 행 또는 잘못된 인덱스입니다. 현재 Go에서 하는 소유자 검사는 `WHERE user_email`로 대체됩니다.
2. 조건부 부모 완료입니다. `markMessageDoneTrue`와 같은 SET 절(상수 하나로 공유)에 `done=0`, `json_array_length(subtasks)>0`, `NOT EXISTS (... json_each ... done IS NOT 1)` 조건을 둡니다. 커밋된 상태를 읽으므로 동시 토글이 수렴하며, “모두 완료인데 부모는 열림”이라는 중간 상태는 유효합니다.

Go bool을 바인딩하면 `1`로 저장되어 `Subtask.Done` 역직렬화가 실패하므로 `json('true')`/`json('false')` 형태를 사용합니다. 두 문장을 하나의 UPDATE로 합치는 안은 SET 표현식이 이전 행을 보므로 권고하지 않습니다.

로컬 SQLite의 deferred read→write 트랜잭션은 `SQLITE_BUSY_SNAPSHOT`과 `RunInTx` 재시도(중복 무효화 포함)를 유발합니다. 원격 libsql HTTP 트랜잭션은 왕복이 약 5회이고 격리 동작이 미확인입니다. 왕복 수와 지연(약 900~1800ms)은 기존 코드 주석의 추정이며 이 경로에서 측정하지 않았습니다.

| 단계 | 범위 | 해결 | 선행 | 비용 | 순서 |
| --- | --- | --- | --- | --- | --- |
| S1 | [store/cache_store.go](store/cache_store.go) `Invalidate*`/`Refresh*`에 사용자별 generation. 무효화 시 `cacheMu` 아래에서 증가, refresh는 조회 전 snapshot을 떠 게시 직전에 비교 | R2 | 없음 | 낮음 | 1 (S2와 병렬) |
| S2 | `updateSubtaskStatusInternal`을 위 두 문장으로 교체. `UpdateSubtaskStatus` 시그니처와 핸들러 유지, 부모 완료 실패 시 `Warnf` 동작 유지 | R1 | 없음 | 낮음~중간 | 1 (S1과 병렬) |
| S3 | `TasksService.ToggleSubtask` 추가, 핸들러가 `a.Tasks` 호출. 서비스가 완료 시도 여부를 판단하고 Store는 가드된 쓰기만 담당. 응답에 `task:{id,done,completed_at}`를 `user`와 함께 추가 | R4 | S2, 정책 (a)(b) | 낮음 | 2 |
| S4 | `onToggleSubtask`가 반환된 부모 상태를 적용하고 `updateMessageCard` 및 사이드바 카운트 갱신 | R4, R6 | S3, 정책 (c) | 낮음 | 3 |
| S5 | `handleResolve`의 `runTaskTx` 반환 후 `InvalidateCache` 재호출(훅 프레임워크 없음) | R2 | S1 | 낮음 | 보류 |

- S1은 R4와 무관합니다. 커밋 후 무효화만으로는 부족하고 generation 검사가 핵심이라는 점은 R2 정정 절과 같습니다. S2 이후 subtask 경로는 autocommit 뒤에 무효화하므로 S1만으로 충분합니다.
- S5는 S1의 테스트에서 커밋 전 창이 실제로 문제로 확인될 때만 진행합니다.
- R6의 `setTaskDone` 통합은 R4에서 분리하여 별도 작업으로 둡니다.

**S2 테스트 요건**
- 인덱스 0, 1을 순차 변경하면 둘 다 저장되고 부모가 닫힙니다.
- 범위 밖 인덱스와 타 사용자 이메일은 오류를 반환합니다.
- 완료 해제는 부모를 다시 열지 않습니다(현재 동작 유지).
- 동시성 테스트는 testMode의 `MaxOpenConns(1)`에서는 의미가 없으므로 file DSN을 사용합니다.
- 같은 케이스를 실제 libsql HTTP 드라이버로도 실행합니다([store/message_deadline_driver_test.go](store/message_deadline_driver_test.go) 방식). `json_set`·`json_each`와 `true` 저장 형식을 원격에서 확인합니다.
- 동적 JSON 경로는 sqlc 정적 분석에 맞지 않을 수 있으며, 이 경우 raw SQL을 사용합니다(선례: `markMessageDoneTrue`). SQL 주석은 ASCII만 사용합니다.

**제품 결정 질문**

| ID | 질문 | 권고 | 필요 시점 |
| --- | --- | --- | --- |
| a | 부모 완료 실패 시 정책 | 200 반환, 실제 부모 상태를 다시 읽어 응답에 담고 `Errorf` 기록 | S3 전 |
| b | 응답 계약에 `task` 추가 허용 여부 | 추가형(기존 클라이언트 호환) | S3 전 |
| c | 자동 완료 시 `TASK_COMPLETED` 이벤트 발생 여부 | 미정 | S4 전 |
| d | 자동 완료를 학습 신호로 칠지 | S3에 넣지 않고 별도 후속 단계로 분리 | 학습 변경 전 |

**원 제안에 대한 지적**
- 부모 자동 완료 규칙이 Store에 있는 것은 “Store에 비즈니스 로직 금지” 규칙에 어긋나며 S3가 해소합니다.
- 핸들러의 `recordUneditedCompletionIfLoaded`([handlers/handlers_msgs.go](handlers/handlers_msgs.go):124)는 비즈니스 후속 처리가 핸들러에 있는 사례입니다. 지금은 이동하지 않습니다(Open).
- `withTx`의 `nil` 분기는 사실상 죽은 코드이고 `withTx`와 `runTaskTx`가 `*sql.DB`를 다르게 해석합니다(Open).

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

### 교차검토 정정 (2026-10-29)

판정: 확인. 줄번호와 발생 조건을 정정합니다.

- 위치 정정: AI 초기화 실패의 조기 `return`은 [scanner/scanner_wiring.go](scanner/scanner_wiring.go):30-33이며 건너뛰는 범위는 :40-51입니다(slackClient·reminderSvc :40-43, exclusionSvc·pastEventSvc :50-51). `:69`는 `WireDailyDigest`입니다. exclusion과 past-event는 AI에 의존하지 않으므로 의도하지 않은 결합입니다.
- 발생 조건: `pc.Enabled()`가 참인데 `NewAIClient`가 오류를 반환할 때만 발동합니다. AI가 단순 미설정이면 해당 블록을 건너뛰고 나머지 초기화는 실행됩니다.
- `WireDailyDigest` 비활성 조건은 slackClient 부재(:70) 외에 reportsSvc 부재도 있으며, reportsSvc는 main의 별도 AI 초기화에서 옵니다.
- 중복 범위: 생성은 [main.go](main.go):149-168(`initAIServices`)과 [scanner/scanner_wiring.go](scanner/scanner_wiring.go):13-39에서 각각 이루어지고 ProviderConfig 필드도 중복됩니다(main.go:132-145, scanner_wiring.go:16-27). scanner의 `tasksSvc`는 API의 `tasksSvc`와 다른 인스턴스이므로 `TasksService`의 다른 인스턴스 상태도 분리됩니다. singleflight·semaphore(동시 5)는 공유되지 않으며 패키지 수준 공용 limiter는 없습니다.
- 누락: [main.go](main.go):127(`initSlackBot`)이 scanner와 별개의 세 번째 `SlackClient`를 만듭니다. R5 범위 포함 여부를 정해야 합니다.
- 인스턴스 분리 유지는 코드에 의도 근거가 없는 가설입니다. 설정 공통 factory만 먼저 도입하는 안이 안전합니다. 조기 `return`을 AI 의존 필드에만 한정하는 수정은 낮은 비용으로 타당합니다.

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

### 교차검토 정정 (2026-10-29)

판정: 부분 확인. 중복과 수정안은 타당하고, 줄번호를 정정하며 불일치 항목을 구체화합니다.

- 확인: `onToggleDone`([src/app.ts](src/app.ts):63-85)과 `onConfirmCandidate`(:129-143)는 사실상 동일하며 `if (done)` emit 조건만 다릅니다.
- 위치 정정: 전체 카드 컴포넌트는 `MessageCard`([src/components/message-card.ts](src/components/message-card.ts):35)이고 `:108`은 완료 후보 배너 조건입니다.
- 부분 갱신(`updateTaskNodeStatus`, [src/renderer.ts](src/renderer.ts):411)은 `--done` 클래스, 버튼 아이콘, 카운트만 바꿉니다. 전체 카드와 달라지는 항목은 다음과 같습니다.
  1. 완료 후보 배너(`!done && candidate.pending`, :108-109)가 남습니다. 서버는 `markMessageDoneTrue`에서 `completion_candidate`를 지우지만 클라이언트 상태는 지우지 않으며 app.ts:127-128의 “banner disappears” 주석은 다음 전체 렌더 전까지 사실이 아닙니다.
  2. exclusion 배너(:122)도 같습니다.
  3. stale 뱃지(`!done` 조건, :70-80)가 남습니다.
  4. 마감 뱃지는 done이면 `''`인데([src/logic.ts](src/logic.ts):176) 부분 갱신 후에도 남습니다.
  5. 완료 해제 시 배너·마감·stale 뱃지를 다시 붙이지 못합니다.
  6. `completed_at`을 클라이언트가 `new Date()`로 설정하여([src/state.ts](src/state.ts):153) 서버 값과 다릅니다.
- 토글 방향을 DOM 클래스에서 읽으므로([src/renderer.ts](src/renderer.ts):99-100) 부분 갱신이 어긋나면 다음 클릭도 영향을 받습니다.
- 롤백: 두 완료 핸들러는 `!done`으로 되돌리고 :65에서 잡은 `oldTask`를 사용하지 않습니다. 완료 해제 실패 시 `completed_at`이 원래 값이 아니라 현재 시각이 됩니다. subtask 핸들러는 이미 `previousDone` 복원과 `updateMessageCard` 호출(:259-260)을 하므로 snapshot 복구 패턴이 코드에 존재합니다.
- `updateMessageCard`([src/renderer.ts](src/renderer.ts):279)는 app.ts:260, 338, 361, 368에서 이미 재사용되어 재사용이 가능합니다. 다만 `outerHTML` 교체는 열린 `<details>`와 편집 폼 입력을 잃을 수 있으므로 교체 전 보존·복원 방안이 필요합니다.
- `setTaskDone` 경로는 snapshot → 상태 변경 → `updateMessageCard` → 실패 시 snapshot 복구 순서로 하고, 이 경로에서 완료 후보 metadata도 함께 지웁니다.

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
