# WhatsApp Messages API

**Base URL**: `https://34.67.133.18.nip.io`  
**Auth**: `Authorization: Bearer <WA_QUERY_TOKEN>` (env: `WA_QUERY_TOKEN`)  
**No OAuth required.**

---

## GET /api/wa/messages

WhatsApp 수/발신 메시지를 조회합니다. 모든 파라미터는 optional.

### Query Parameters

| 파라미터 | 타입 | 설명 | 예시 |
|----------|------|------|------|
| `date` | `YYYY-MM-DD` | 특정 날짜 하루치 (Asia/Seoul). `from`/`to`보다 우선 | `2026-05-25` |
| `from` | RFC3339 | 시작 시각 | `2026-05-01T00:00:00+09:00` |
| `to` | RFC3339 | 종료 시각 | `2026-05-31T23:59:59+09:00` |
| `chat_jid` | string | 채팅방 JID 필터 | `120363xxx@g.us` |
| `direction` | string | `incoming` 또는 `outgoing` | `incoming` |
| `email` | string | 계정 이메일 (멀티 계정 시) | `jjsong@whatap.io` |
| `limit` | int | 반환 건수 (기본 50, 최대 200) | `100` |
| `offset` | int | 페이지네이션 오프셋 | `50` |

### Response

```json
{
  "messages": [
    {
      "id": 1,
      "message_id": "3EB0...",
      "email": "jjsong@whatap.io",
      "chat_jid": "120363xxx@g.us",
      "chat_name": "팀 채널",
      "sender": "821012345678@s.whatsapp.net",
      "direction": "incoming",
      "body": "안녕하세요",
      "reply_to": "",
      "has_attachment": 0,
      "is_forwarded": 0,
      "mentions": "[]",
      "ts": 1748131200,
      "created_at": "2026-05-25T10:00:00"
    }
  ],
  "count": 1,
  "offset": 0
}
```

### 필드 설명

| 필드 | 설명 |
|------|------|
| `chat_jid` | WhatsApp 채팅방 고유 ID. 개인: `번호@s.whatsapp.net`, 그룹: `숫자@g.us` |
| `chat_name` | 채팅방 이름 (그룹명 또는 JID 그대로) |
| `sender` | 발신자 JID |
| `direction` | `incoming`=수신, `outgoing`=발신 |
| `has_attachment` | `1`=첨부 있음, `0`=없음 |
| `is_forwarded` | `1`=전달됨, `0`=아님 |
| `mentions` | JSON 배열 `["이름1","이름2"]` |
| `ts` | Unix timestamp (초) |

---

## 사용 예시

```bash
TOKEN="32dce58c3e28872f6263a0a2fda59c9d3ddf4835ac33b4ec17f128898eabf79a"
BASE="https://34.67.133.18.nip.io"

# 오늘 하루치 전체
curl -H "Authorization: Bearer $TOKEN" "$BASE/api/wa/messages?date=2026-05-25"

# 특정 채팅방 + 날짜
curl -H "Authorization: Bearer $TOKEN" "$BASE/api/wa/messages?chat_jid=120363xxx@g.us&date=2026-05-25"

# 수신 메시지만, 최근 100건
curl -H "Authorization: Bearer $TOKEN" "$BASE/api/wa/messages?direction=incoming&limit=100"

# 날짜 범위 조회
curl -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/wa/messages?from=2026-05-01T00:00:00%2B09:00&to=2026-05-31T23:59:59%2B09:00"

# 다음 페이지
curl -H "Authorization: Bearer $TOKEN" "$BASE/api/wa/messages?date=2026-05-25&limit=50&offset=50"
```

---

## DB 스키마 (`wa_messages`)

```sql
id             INTEGER PK AUTOINCREMENT
message_id     TEXT UNIQUE          -- dedup key
email          TEXT                 -- 계정 이메일
chat_jid       TEXT                 -- 채팅방 JID
chat_name      TEXT                 -- 채팅방 이름
sender         TEXT                 -- 발신자 JID
direction      TEXT                 -- incoming / outgoing
body           TEXT                 -- 메시지 본문
reply_to       TEXT                 -- 답장 대상 이름
has_attachment INTEGER              -- 0/1
is_forwarded   INTEGER              -- 0/1
mentions       TEXT                 -- JSON array
ts             INTEGER              -- Unix timestamp
created_at     TEXT                 -- DB 삽입 시각
```

**인덱스**:
- `(message_id)` UNIQUE — INSERT OR IGNORE dedup
- `(ts)` — date= 단독 / from~to range scan
- `(chat_jid, ts)` — 채팅방 필터 + 날짜 range + ORDER BY ts DESC 복합 커버

---

## 구현 파일

| 역할 | 파일 |
|------|------|
| 핸들러 + 인증 미들웨어 | `handlers/wa_messages_handler.go` |
| 라우트 등록 | `handlers/routes.go` — `registerWAQueryRoutes` |
| 저장 서비스 | `services/wa_db_logger.go` — `WADBLogger.Receive` |
| Store 함수 | `store/wa_messages_store.go` |
| sqlc 쿼리 | `store/queries/wa_messages.sql` |
| 생성된 코드 | `db/wa_messages.sql.go` |
| 마이그레이션 | `store/migrations.go` — schemaVersion 8 |
| 설정 | `config/config.go` — `WAQueryToken` (`WA_QUERY_TOKEN`) |
