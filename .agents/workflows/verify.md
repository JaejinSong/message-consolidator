# Antigravity Regression Verification Workflow

이 워크플로우는 배포 전 주요 비즈니스 로직 및 UI 렌더링 방식의 변경 사항을 검증하기 위한 절차입니다.
가급적 브라우저 테스트 전, **스크립트 기반 검증**을 우선 수행하십시오.

---

## 1. 개요 (Overview)
- **대상**: JS 순수 로직 (`logic.js`), DOM 렌더링 (`renderer.js`), 백엔드 엔드포인트
- **목적**: 탭 필터링, 검색, 소급 매핑(Normalize), 업적/토큰 통계 등 핵심 기능 보장

---

## 2. 스크립트 기반 검증 (Node.js) // turbo-all

> [!NOTE]
> `logic.js`와 `renderer.js`는 DOM에 의존하지 않거나 Mocking이 가능하도록 설계되어 있습니다.

### 2.1. 비즈니스 로직 검증 (`verify_logic.js`)
- 정렬/필터링 (생성일 소급 적용 포함)
- 카테고리 분류 및 히트맵 레벨 계산 로직
```bash
node static/js/verify_logic.js && echo "[PASS] JS Logic"
```

### 2.2. 렌더러 Mock 검증 (`verify_renderer.js`)
- 사용자 프로필 시각화 (XP, 레벨, 스트릭 아이콘)
- 릴리즈 노트 마크다운 파싱 로직
- 아카이브 테이블 구조 (Room Badge, 보관 정책 등)
- 탭별 동적 렌더링 및 `created_at` 소급 처리
```bash
node static/js/verify_renderer.js && echo "[PASS] Renderer Mock"
```

---

## 3. 백엔드 및 통합 검증 (Shell Script)

### 3.1. API 응답 형식 검증
- WhatsApp 연결 상태 (소문자 `connected` 유지 여부)
```bash
grep -q 'return "connected"' channels/whatsapp.go && echo "[PASS] Backend Status Consistency"
```

### 3.2. 데이터 정규화(Normalize) 검증
- 할당자 및 이메일 전처리 로직 유지 여부
```bash
grep -q "store.NormalizeName" handlers/handlers_msgs.go && echo "[PASS] Name Normalization"
grep -q "strings.TrimSpace.*strings.ToLower" auth/auth.go && echo "[PASS] Email Normalization"
```

---

## 4. 실시간 로그 모니터링
배포 후 VPS에서 다음 명령어를 통해 실시간 이상 징후를 감시합니다.
```bash
# Handler 500 에러 감시 (업적/토큰 관련 로깅 추가됨)
docker logs -f message-consolidator_app_1 2>&1 | grep "\[HANDLER\]"
```

---
*마지막 업데이트: 2026-03-22*