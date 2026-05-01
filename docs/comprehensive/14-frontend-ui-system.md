# 14. Frontend UI System / 프론트엔드 UI 시스템

> Cross-ref: → [13-frontend-architecture.md] (아키텍처 전체 구조 · 모듈 분리 원칙)  
> Cross-ref: → [19-user-manual.md] (최종 사용자 기능 설명)

---

## 1. UI 시스템 개요 / Overview

### 1.1 Components vs Renderers 구분

`src/` 레이어는 **재사용 가능 컴포넌트**와 **페이지·섹션 렌더러**로 이분된다.

| 디렉토리 | 역할 | 예시 |
|---|---|---|
| `src/components/` | 독립 생명주기를 가진 재사용 위젯 | `Combobox`, `MessageCard`, `TokenUsageCard` |
| `src/renderers/` | 특정 탭·모달 한 곳에서만 쓰이는 섹션 담당자 | `admin-renderer.ts`, `connections-renderer.ts` |

컴포넌트는 `new ClassName(container, options)` 또는 순수 함수(`MessageCard(props): string`) 형태로 노출한다.
렌더러는 DOM `id`를 직접 조회하고 섹션 범위 밖을 침범하지 않는다.

### 1.2 CSS 토큰 + BEM

모든 스타일은 `static/css/variables.css`에 선언된 CSS 커스텀 프로퍼티(토큰)를 소비한다.
`px`/`#hex` 하드코딩은 `CLAUDE.md` 위반이므로 `rem`과 `var(--*)` 토큰만 허용된다.
클래스 명명은 BEM `block__element--modifier` 방식이며, 재사용 컴포넌트는 `c-` 프리픽스를 붙인다.

### 1.3 i18n 4언어

`src/locales/` 아래 `ko.ts`, `en.ts`, `id.ts`, `th.ts` 4개 파일이 존재한다.
런타임에 `i18n-renderer.ts`의 `renderUILanguage(lang)` 함수 한 번 호출로 전체 DOM 텍스트가 교체된다.

---

## 2. Components (재사용 위젯)

### 2.1 `MessageCard` — `src/components/message-card.ts`

**형태**: 순수 함수 컴포넌트. 상태를 갖지 않고 `props → HTML string`을 반환한다.

```typescript
export type MessageCardProps = Message & {
    lang: string;
    isSelected?: boolean;
    currentUserNames?: string[];
    staleThresholdWorkingDays?: number;
};
export function MessageCard(props: MessageCardProps): string { ... }
```

**BEM 블록**: `.c-message-card`

주요 모디파이어:

| 클래스 | 조건 |
|---|---|
| `--done` | `props.done === true` |
| `--loading` | `is_translating` 진행 중 |
| `--policy` | `category === 'POLICY'` |
| `--query` | `category === 'QUERY'` |
| `--shared` | `assignee === ASSIGNEE_SHARED` |
| `--context` | 메타데이터 `is_context_query` |

**Stale 배지 계산**: 백엔드에 `stale` 상태 컬럼이 없으므로 뷰 레이어에서 `TimeService.getWorkingDaysSince(created_at)`로 영업일 경과를 직접 계산한다. 임계값은 `staleThresholdWorkingDays` props로 주입된다.

**이벤트**: 카드 내부 버튼은 `data-action` 어트리뷰트로 식별된다 (`select-task`, `delete`, `toggle-done`, `show-original`, `map-alias`, `toggle-subtask`). 이벤트 위임은 상위 컨테이너가 담당한다.

**사용처**: `app.ts`의 대시보드 렌더 루프, 아카이브 테이블 row 생성.

---

### 2.2 `Combobox` — `src/components/combobox.ts`

**형태**: 클래스 컴포넌트. 생성 시 `container` DOM 요소와 `ComboboxOptions`를 받고 자체 DOM을 `innerHTML`로 주입한다.

```typescript
export class Combobox implements ComboboxInterface {
    constructor(container: HTMLElement, options: ComboboxOptions) { ... }
    getValue(): AccountItem | null { ... }
    clear(): void { ... }
    destroy(): void { ... }  // Why: listener leak 방지를 위해 명시적 정리 필수
}
```

**BEM 블록**: `.c-combobox`

핵심 옵션:

| 옵션 | 설명 |
|---|---|
| `searchFn` | `async (query: string) => AccountItem[]` — 비동기 검색 주입 |
| `onSelect` | 선택 시 콜백 |
| `debounceMs` | 기본값 250ms. 검색 API 과호출 방지 |
| `renderItem` | 아이템 HTML 커스터마이징 (default: title + subtitle 2줄) |

키보드 네비게이션 (`ArrowDown`, `ArrowUp`, `Enter`, `Escape`) 과 외부 클릭 시 드롭다운 닫기를 지원한다.

**Why 클래스**: `destroy()`로 `document.addEventListener`로 등록한 click 핸들러를 명시적으로 제거해야 한다. SPA 탭 전환 시 컴포넌트가 DOM에서 제거되어도 document-level 핸들러는 살아남기 때문이다.

**사용처**: 설정 탭의 계정 연결(Account Linking) 폼에서 담당자 검색.

---

### 2.3 `TokenUsageCard` — `src/components/token-usage.ts`

**형태**: 클래스 컴포넌트. `containerId`를 받아 `document.getElementById`로 마운트한다.

```typescript
export class TokenUsageCard {
    constructor(containerId: string = 'tokenUsageContainer') { ... }
    render(data: TokenUsage): void { ... }
    destroy(): void { ... }
}
```

**BEM 블록**: `.c-token-usage`

오늘 사용량(IN/OUT 분리), 월간 합계, 예상 비용(`Intl.NumberFormat currency USD`)을 2개 카드로 렌더링한다. 모든 수치는 백엔드가 계산해서 전달하므로 프론트엔드는 포맷팅만 담당한다.

**Why Intl.NumberFormat**: `locale: undefined` 지정으로 브라우저 기본 로케일 숫자 포맷을 따른다. 하드코딩된 콤마(`,`) 구분자는 로케일에 따라 오작동한다.

**사용처**: Insights 탭 내 `tokenUsageContainer` 요소.

---

## 3. Renderers (페이지/섹션)

### 3.1 `status-renderer.ts`

**책임**: 대시보드 상단 서비스 상태 카드(ON/OFF) + QR 모달 상태 머신 + 채널별 연결/연결해제 섹션 전환.

핵심 함수:

| 함수 | 역할 |
|---|---|
| `updateServiceStatusUI(service, status)` | `c-status-card--active/--inactive` 토글, 설정 pill 연동 |
| `updateWhatsAppQR(status, data, lang)` | QR 상태 4단계 (`generating → show → success → error`) |
| `updateQRTimer(remaining, total)` | Progress bar width를 `(remaining/total)*100%`로 갱신 |
| `updateTelegramStatus(status)` | Telegram은 4값 enum — `connected`만 ON으로 간주 |

**Why 분리**: WhatsApp의 QR 갱신 타이머와 Gmail/Telegram 연결 섹션은 로직이 달라서 `updateServiceStatusUI` 공통 경로와 서비스별 분기를 함께 보유한다.

---

### 3.2 `connections-renderer.ts`

**책임**: Settings → Connections 탭의 4개 채널 카드(`c-connection-card`) 렌더링 및 액션 핸들러.

```typescript
export interface ConnectionsState {
    gmail: { connected: boolean; email?: string };
    whatsapp: { connected: boolean; deviceName?: string };
    telegram: { status: string; hasCredentials?: boolean; ... };
    slack: { connected: boolean; slackId?: string };
}
```

초기화는 `setupConnectionsTab()`을 한 번 호출하면 skeleton HTML을 주입하고 이벤트를 바인딩한다. 이후 `renderConnections(snapshot)` 호출로 상태를 갱신하며, 언어 변경 시 `rerenderConnections()`로 캐시된 스냅샷을 재사용한다.

**Why 캐시**: 연결 상태 폴링 데이터를 Connections 탭에서 다시 fetch하지 않도록 마지막 스냅샷을 모듈-level `cachedState`에 보관한다. 언어 전환 시 네트워크 요청 없이 즉시 재렌더링 가능하다.

Slack 카드는 write 액션이 없으므로 `setActions(card, [], lang)`으로 버튼을 비운다.

---

### 3.3 `admin-renderer.ts`

**책임**: Admin 탭의 설정 그룹 렌더링 + 관리자 계정 CRUD.

설정을 `category`별로 그룹화(`auth` / `ai` / `channels` / `db` / `ops`)하고 각 항목을 `<form data-submit>`으로 래핑한다. `secret` 타입 항목은 `input[type=password]`로 렌더링하고 기존 값을 placeholder `••••`로 표시한다.

**초기화 패턴**: `bindOnce()`에서 `initialized` 플래그로 이벤트 바인딩 중복을 막는다 — 탭을 반복해서 전환해도 핸들러가 누적되지 않는다.

**사용처**: Admin 탭은 `profile-renderer.ts`가 `is_admin || is_super_admin` 여부로 탭 버튼 가시성을 제어하므로 일반 사용자에게는 노출되지 않는다.

---

### 3.4 `settings-renderer.ts`

**책임**: Settings → Identity 탭의 AI 병합 제안(Proposal) 카드 렌더링.

```typescript
export function renderProposals(
    proposals: ProposalGroup[],
    onAccept: (groupId: string, canonicalName: string) => void,
    onReject: (groupId: string) => void
): void
```

각 제안은 `c-proposal-card` 블록으로 렌더링되며, 연결 후보 이름 칩과 신뢰도 퍼센트, 대표 이름 select + 수락/거절 버튼을 포함한다.

**Why DOM 쿼리 방식**: `querySelectorAll` + `data-group` 어트리뷰트로 클로저 대신 데이터-드리븐 바인딩. 제안 수가 많을 때 클로저 누적을 피한다.

---

### 3.5 `reports-renderer.ts`

**책임**: Insights 탭의 AI 생성 보고서 시각화. SVG 네트워크 그래프, 데이터 테이블, 마크다운 파싱 출력.

주요 기능:
- **SVG 네트워크 그래프**: `createElementNS(SVG_NS, ...)` 기반 노드-링크 다이어그램. 노드 색상은 `me`(warning) / `internal`(success) / `external`(primary) 3분류.
- **툴팁**: `document.body`에 `#report-tooltip(.c-insights-tooltip)` 싱글톤 생성. 뷰포트 경계 충돌을 계산해 위치를 자동 보정한다.
- **토큰**: 툴팁 스타일을 `--bg-floater`, `--text-main`, `--spacing-*`, `--radius-sm`, `--shadow-card`, `--z-index-modal` 토큰으로 구성 — `px`/`hex` 없음.

---

### 3.6 `profile-renderer.ts`

**책임**: 사용자 프로필(이메일·아바타) 표시 + 관리자 탭 노출 제어.

```typescript
export function updateUserProfile(profile: UserProfile | null): void
```

`is_admin || is_super_admin` 값에 따라 `[data-settings-tab="adminTab"]` 버튼의 `hidden` 클래스를 토글한다. 게임화(Gamification) 컨테이너는 feature 제거 후 `hidden`으로 고정되어 있다.

---

### 3.7 `i18n-renderer.ts`

**책임**: `renderUILanguage(lang)` 단일 진입점. DOM 전체 i18n 텍스트 교체.

3가지 어트리뷰트 패턴:

| 어트리뷰트 | 적용 프로퍼티 |
|---|---|
| `data-i18n` | `element.textContent` |
| `data-i18n-title` | `element.title` |
| `data-i18n-placeholder` | `input.placeholder` |

탭 레이블은 카운트 배지(`<span id="archiveCount">`)를 덮어쓰지 않도록 `applyTabLabel` 내부에서 `[data-i18n]` 하위 요소만 업데이트한다.

**Why 별도 렌더러**: 번역 로직(`i18n.ts`의 `t(key, lang)`)과 DOM 조작을 분리한다. `i18n.ts`는 순수 함수이므로 테스트에서 DOM 없이 호출 가능하다.

---

### 3.8 `telegram-modal-renderer.ts`

**책임**: Telegram MTProto 4단계 인증 플로우 모달 DOM 상태 머신.

```typescript
type StepId = 'credentials' | 'phone' | 'code' | 'password' | 'connected';
```

**단계 전환 규칙** (`syncTelegramModalToStatus`):

```
status=connected         → 'connected' step
!hasCredentials          → 'credentials' step (App ID/Hash 미입력 시 phone 제출 불가)
status=pending_password  → 'password' step
status=pending_code      → 'code' step
else                     → 'phone' step
```

`showStep(id)` 호출 시 5개 div를 순회하며 한 개만 `hidden` 해제한다.
`bindTelegramModal()` 는 `root.dataset.bound === '1'` 가드로 멱등성을 보장한다.
Enter 키 → 다음 버튼 트리거 바인딩은 각 입력-버튼 쌍에 `enterSubmit` 헬퍼로 적용된다.

**사용처**: `connections-renderer.ts`의 `tg-connect`, `tg-reauth`, `tg-change-creds` 액션.

---

### 3.9 `task-renderer.ts`

**책임**: 메시지 카드 헤더의 소스 채널 아이콘 배지 렌더링.

```typescript
export function renderSourceList(channels?: string[], fallback?: string): string
```

`Set`으로 중복 제거 후 채널별 SVG 아이콘(`ICONS`)을 `task-card__source-icon--{channel}` 클래스 배지로 변환한다. `aria-label`로 접근성 레이블을 추가한다.

---

## 4. CSS 토큰 시스템 (variables.css)

`static/css/variables.css`는 `:root` 아래 전체 토큰을 정의하고 `body.light-theme` 블록에서 필요한 것만 오버라이드한다.

### 4.1 토큰 카탈로그

**색상 (Color)**

| 토큰 | 다크 기본값 | 설명 |
|---|---|---|
| `--bg-color` | `#050608` | 페이지 배경 |
| `--card-bg` | `rgba(255,255,255,0.03)` | 카드 배경 (글래스) |
| `--accent-color` | `#00f2ff` | 주요 강조색 |
| `--text-main` | `#f0f0f0` | 기본 텍스트 |
| `--text-dim` | `#9ca3af` | 보조 텍스트 |
| `--glass-border` | `rgba(255,255,255,0.08)` | 카드 테두리 |

**시맨틱 색상**

| 토큰 | 다크 | 라이트 |
|---|---|---|
| `--color-success` | `#10b981` | `#059669` |
| `--color-warning` | `#fbbf24` | `#d97706` |
| `--color-error` | `#ff3b30` | `#dc2626` |
| `--color-info` | `#3b82f6` | `#2563eb` |

**채널 브랜드 색상** (테마 비의존, 고정값)

| 토큰 | 값 |
|---|---|
| `--color-slack` | `#36C5F0` |
| `--color-whatsapp` | `#25D366` |
| `--color-gmail` | `#EA4335` |
| `--color-telegram` | `#0088cc` |

**간격 (Spacing)** — 모두 `rem` 기준

`--spacing-xxs` (0.125) → `--spacing-xs` (0.25) → `--spacing-sm` (0.5) → `--spacing-md` (0.75) → `--spacing-lg` (1) → `--spacing-xl` (1.25) → `--spacing-2xl` (1.5) → `--spacing-3xl` (2)

**반경 (Radius)**

`--radius-sm` (0.25) → `--radius-md` (0.5) → `--radius-lg` (0.75) → `--radius-xl` (1) → `--radius-2xl` (1.25) → `--radius-full` (9999px)

**레이어링 (Z-Index)**

| 토큰 | 값 |
|---|---|
| `--z-index-sticky` | 10 |
| `--z-index-dropdown` | 1000 |
| `--z-index-modal` | 2000 |
| `--z-index-spinner` | 10000 |

**컴포넌트 전용 토큰** (크기 맥락)

| 토큰 | 값 |
|---|---|
| `--status-card-size` | 6.25rem |
| `--task-card-min-width` | 25rem |
| `--btn-size` | 2.5rem |
| `--col-room` | 7.5rem |
| `--col-time` | 9.375rem |

### 4.2 토큰을 쓰는 이유

- **다크/라이트 테마 전환**: `body.light-theme` 오버라이드만으로 전체 색상 교체. 소비자 CSS는 토큰명만 알면 되어 테마 추가 비용이 선형적이다.
- **일관성 강제**: 간격 토큰 step을 벗어난 임의 값(`0.6rem` 등)은 코드 리뷰에서 즉시 식별된다.
- **rem 강제 이유**: `px`는 브라우저 폰트 크기 설정을 무시한다. 사용자 접근성 설정(예: 기본 16px → 20px 변경)이 레이아웃에 반영되려면 `rem` 기준이어야 한다.

---

## 5. BEM 규칙

### 5.1 표준 패턴

```
.c-[block]__[element]--[modifier]
```

- **Block**: 독립적으로 의미 있는 컴포넌트 단위. 예: `c-message-card`, `c-combobox`, `c-btn`.
- **Element**: 블록 내부 구성 요소. 예: `c-message-card__header`, `c-combobox__input`.
- **Modifier**: 상태 또는 변형. 예: `c-message-card--done`, `c-btn--primary`.

### 5.2 `c-*` 프리픽스

재사용 컴포넌트는 `c-` 프리픽스로 일반 페이지 클래스와 구분한다.

`static/css/components/` 아래 파일별 담당 블록:

| 파일 | 블록 |
|---|---|
| `buttons.css` | `.c-btn`, `.c-btn--primary`, `.c-btn--ghost` 등 |
| `badge.css` | `.c-badge`, `.c-badge--priority-*` |
| `combobox.css` | `.c-combobox`, `.c-combobox__input`, `.c-combobox__item--active` |
| `message-card.css` | `.c-message-card` 전체 블록 |
| `status-card.css` | `.c-status-card`, `--active`, `--inactive` |
| `telegram-modal.css` | `.c-modal` (Telegram 전용 확장) |
| `token-usage.css` | `.c-token-usage`, `.c-token-usage__card` |
| `spinners.css` | `.c-spinner`, `.c-spinner--sm` |
| `inputs.css` | `.c-input` |
| `tabs.css` | `.c-tabs`, `.tab-btn`, `.c-settings__tab--active` |
| `utilities.css` | `.u-text-dim`, `.u-text-xs` 등 유틸리티 |

### 5.3 `v2-*` 파일의 의도

`v2-nav.css`, `v2-modals.css`, `v2-settings.css`, `v2-connections.css`, `v2-insights.css`는 초기 단일 `style.css`를 섹션별로 분리할 때 버전 충돌을 피하기 위해 `v2` 네임스페이스를 붙였다. 내부 클래스는 동일하게 `c-*` 패턴을 따른다.

### 5.4 설계 원칙

- Element는 블록의 직계 자식에만 붙인다. 예: `.c-combobox__menu` 안의 아이템은 `.c-combobox__item`이며, `.c-combobox__menu__item`(2중 `__`)는 잘못된 형태다.
- Modifier는 Element에도 적용 가능하다. 예: `.c-combobox__item--active`.
- 상태 클래스(`hidden`, `active`)는 JS로 토글하는 헬퍼 클래스이며 BEM 모디파이어와 공존한다.

---

## 6. i18n

### 6.1 파일 구조

```
src/locales/
├── ko.ts   — 한국어
├── en.ts   — 영어
├── id.ts   — 인도네시아어
└── th.ts   — 태국어
```

각 파일은 `I18nEntry` 타입을 구현한 named export다 (`export const ko: I18nEntry = { ... }`).

### 6.2 키 구조

키는 camelCase 평탄 구조다. 계층 없이 `I18nEntry` 인터페이스에 선언된 키를 모든 언어 파일이 같은 이름으로 구현한다.

주요 키 범주:

| 범주 | 예시 키 |
|---|---|
| 채널 상태 | `slackMonitoring`, `waConnected`, `statusOn` |
| 테이블 헤더 | `hSource`, `hRoom`, `hTask`, `hAssignee`, `hTime` |
| 탭 레이블 | `receivedTasks`, `delegatedTasks`, `referenceTasks`, `allTasks` |
| 설정 | `settingsTitle`, `accountLinkingTitle`, `tokenMenuTitle` |
| 연결 탭 | `connStatusConnected`, `connConnectBtn`, `connDisconnectBtn` |
| 토스트/모달 | `waStatusConnectedToast`, `logoutConfirm`, `qrError` |

### 6.3 i18n 적용 메커니즘

`renderUILanguage(lang)` (→ `i18n-renderer.ts`) 호출 순서:

1. `applyDataAttributes(lang)` — `[data-i18n]`, `[data-i18n-title]`, `[data-i18n-placeholder]` 전체 스캔
2. `applyNavLink(...)` — `.c-main-nav__item` 텍스트 갱신
3. `applyTabLabel(...)` — 탭 버튼 레이블 갱신 (count 배지 보존)
4. `applyArchiveHeader(lang)` — `#archiveCount` outerHTML을 보존하며 헤더 재조립
5. `applyStatusText(...)` — 각 채널 ON/OFF 텍스트 갱신

`MessageCard`처럼 동적 HTML string을 생성하는 컴포넌트는 `I18N_DATA[lang]` 딕셔너리를 직접 참조한다.

### 6.4 `knowledge/frontend_tab_i18n` 연관성

`knowledge/frontend_tab_i18n/artifacts/setup_tabs_utility.md`는 `setupTabs` 유틸리티 설계 결정을 기록한다. BEM 모디파이어 기반 활성 클래스 관리(`c-settings__tab--active`)와 탭 전환 콜백(`onSwitch`) 패턴이 i18n 렌더러의 탭 레이블 갱신과 함께 동작한다.

---

## 7. 테마 / 다크모드

### 7.1 `body.light-theme` 패턴

테마 전환은 `<body>` 요소에 `light-theme` 클래스를 토글하는 방식이다.

```css
:root { --bg-color: #050608; }          /* 다크 기본 */
body.light-theme { --bg-color: #f1f5f9; } /* 라이트 오버라이드 */
```

컴포넌트 CSS는 `var(--bg-color)` 등 토큰만 소비하므로 테마 전환 코드를 포함하지 않는다.

### 7.2 라이트 테마 오버라이드 범위

`variables.css`에서 라이트 테마가 오버라이드하는 토큰:
- 배경 계열: `--bg-color`, `--card-bg`, `--bg-floater`, `--bg-rgb`
- 텍스트: `--text-main`, `--text-dim`, `--text-muted`, `--text-main-rgb`
- 테두리/그림자: `--glass-border`, `--shadow-main`, `--shadow-xl`, `--shadow-card`
- 시맨틱 색상: `--color-success/warning/error/info` (라이트용 고채도 조정)
- 테이블: `--table-row-hover`, `--table-header-bg`
- 히트맵: `--color-heatmap-0~4`
- 브랜드 별칭: `--brand-excel`, `--brand-whatsapp`, `--brand-json`

채널 브랜드 색상(`--color-slack`, `--color-gmail` 등)은 라이트 오버라이드 없음 — 공식 브랜드 색상이라 테마 무관이다.

### 7.3 `data-theme` 어트리뷰트 부재

현재 구현은 CSS class 방식(`body.light-theme`)을 사용한다. `data-theme` 어트리뷰트 방식으로 아직 마이그레이션되지 않았다. 향후 확장 시 `:root[data-theme="light"]` 패턴 도입을 고려할 수 있다.

---

## 8. 알려진 함정 / Gotchas

### 8.1 `px` 하드코딩 검출

`CLAUDE.md` 규칙상 `px` 직접 사용은 금지된다. 유일한 예외는 픽셀 단위가 의미론적으로 요구되는 border (`--border-thin: 1px`, `--border-thick: 6px`)뿐이다.

신규 CSS 작성 후 검출 방법:

```bash
grep -rn '[0-9]px' static/css/components/ | grep -v 'border'
```

### 8.2 `.js` 파일 전환

`CLAUDE.md`에 따라 `.js` 파일 발견 시 즉시 `.ts`로 전환해야 한다. `knowledge/FRONTEND_ARCHITECTURE.md`는 구버전 `.js` 기반으로 작성되어 있어 참고 시 주의가 필요하다. 실제 소스는 `src/**/*.ts`다.

### 8.3 `document.addEventListener` 누수

`Combobox.destroy()` 호출 없이 컴포넌트를 교체하면 `document` 레벨 click 핸들러가 누적된다. 탭 전환으로 컴포넌트가 재생성되는 경우 반드시 이전 인스턴스의 `destroy()`를 먼저 호출해야 한다.

### 8.4 `I18nEntry` 키 누락

신규 i18n 키 추가 시 4개 파일(`ko`, `en`, `id`, `th`) 모두에 추가해야 한다. `I18nEntry` 타입에 필드를 추가하면 TypeScript가 누락 파일에서 컴파일 에러를 발생시킨다.

### 8.5 `hidden` vs CSS 모디파이어 혼용

일부 섹션은 `element.classList.toggle('hidden', condition)`을 사용하고, 다른 곳은 BEM 모디파이어(`c-connection-card--disconnected`)를 사용한다. 신규 코드는 가시성 제어에 `hidden` 유틸리티 클래스를 사용하고 시각적 변형에만 BEM 모디파이어를 사용하는 패턴을 권장한다.

### 8.6 보고서 툴팁 싱글톤

`reports-renderer.ts`의 `showTooltip()`은 `document.body`에 `#report-tooltip` 요소를 최초 호출 시 생성하고 이후 재사용한다. Insights 탭을 떠나도 요소가 DOM에 남아있으므로 필요 시 숨김(`display:none`) 상태를 확인할 것.

---

## 9. Cross-References + Deltas

| 문서 | 연관 내용 |
|---|---|
| → [13-frontend-architecture.md] | 모듈 의존성 그래프, `app.ts` 진입점, Clean Architecture 레이어 |
| → [19-user-manual.md] | 최종 사용자가 보는 각 UI 섹션의 기능 설명 |
| → [05-channels.md] | 채널(Slack/WhatsApp/Gmail/Telegram)별 백엔드 연결 로직 |
| → [09-identity-and-dedup.md] | Combobox로 조작하는 계정 연결(Account Linking) 배경 |

**Deltas (현재 코드와 구버전 문서 차이)**

- `knowledge/FRONTEND_ARCHITECTURE.md`는 `.js` 기반 구조를 기술한다. 현재 소스는 전부 `.ts`이며 `src/components/`와 `src/renderers/`로 재편되어 있다.
- `v2-components.css`가 문서에 언급되지만 실제 파일은 `static/css/components/` 디렉토리로 분할되어 있다.
- 게임화(Gamification) UI는 `profile-renderer.ts`에서 `hidden` 처리로 비활성화되어 있다.

---

*총 줄 수: 약 480줄 | components 3개 (MessageCard · Combobox · TokenUsageCard) · renderers 8개 (status · connections · admin · settings · reports · profile · i18n · telegram-modal) + task-renderer 보조 1개 매핑 완료 | locales 4언어 (ko · en · id · th)*
