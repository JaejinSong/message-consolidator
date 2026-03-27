# Frontend Architecture & Maintenance Guide

본 문서는 `message-consolidator` 프론트엔드 프로젝트의 구조, 설계 철학, 그리고 각 모듈(파일)의 역할을 정의한 유지보수 가이드입니다.

## 1. 설계 철학 (Design Principles)
* **Vanilla JS 기반 모듈화**: 프레임워크(React/Vue) 없이 ES6 모듈 시스템을 활용하여 가볍고 빠른 렌더링을 구현합니다.
* **관심사 분리 (Separation of Concerns)**: 순수 데이터 처리(`logic.js`), DOM 조작(`renderer.js`), 상태 관리(`state.js`), 통신(`api.js`)을 철저히 분리합니다.
* **이벤트 기반 아키텍처 (Event-Driven)**: 모듈 간의 강한 결합을 피하기 위해 `events.js` (Pub/Sub)를 통한 비동기 상태 전파를 사용합니다.
* **BEM & 글래스모피즘 CSS**: CSS는 Block-Element-Modifier 방법론을 따르며, 유지보수가 쉽도록 컴포넌트 단위로 파일을 분할했습니다.

---

## 2. 파일별 역할 (Directory & Modules)

### 🚀 Core (핵심 제어 및 상태)
| 파일명 | 역할 | 상세 설명 |
| :--- | :--- | :--- |
| **`app.js`** | **Entry Point** | 애플리케이션 초기화, 네비게이션 탭 스위칭, 백그라운드 폴링 설정, 전역 이벤트 핸들러 매핑 등을 담당하는 메인 컨트롤러입니다. |
| **`state.js`** | **Global State** | 사용자 정보, 현재 테마, 언어, 아카이브 페이징 상태 등 전역 상태를 저장하고 관리합니다. |
| **`events.js`** | **Event Bus** | 모듈 간 결합도를 낮추기 위한 Pub/Sub 객체입니다. (예: `TASK_COMPLETED` 이벤트 발생 시 UI 및 통계 모듈이 각각 반응) |
| **`api.js`** | **API Layer** | 백엔드와의 모든 `fetch` 통신을 담당하며, `handleResponse`를 통해 401(인증 에러) 등을 중앙에서 처리합니다. |

### 🧠 Logic (비즈니스 로직 및 유틸리티)
| 파일명 | 역할 | 상세 설명 |
| :--- | :--- | :--- |
| **`logic.js`** | **Pure Functions** | 업무 필터링, 정렬, 히트맵 통계 계산, 마크다운 파싱 등 **DOM에 의존하지 않는 순수 비즈니스 로직**입니다. (테스트 용이) |
| **`utils.js`** | **Utilities** | `safeAsync`(에러 핸들링 래퍼), `TimeService`(날짜 포맷팅), `escapeHTML`(XSS 방지) 등 공통 유틸리티 함수 모음입니다. |
| **`constants.js`** | **Constants** | DOM ID, 폴링 주기(`POLLING_INTERVALS`), 상태 텍스트 등 전역 상수 집합입니다. |
| **`taskFilter.js`** | **Filtering** | 담당자 이름 자동 감지 및 검색어 기반 업무 필터링 커스텀 로직입니다. |

### 🎨 UI / Renderers (뷰 및 DOM 조작)
| 파일명 | 역할 | 상세 설명 |
| :--- | :--- | :--- |
| **`renderer.js`** | **Main DOM Controller** | 대시보드의 업무 카드 렌더링, 프로필 업데이트, 토스트 알림 등을 담당합니다. **이벤트 위임(Event Delegation)** 패턴으로 메모리 누수를 방지합니다. |
| **`insightsRenderer.js`**| **Insights DOM** | 인사이트 탭의 히트맵, 차트, 업적(Achievements), 요약 통계 요소들을 DOM에 그리는 역할을 합니다. |
| **`insights.js`** | **Insights Controller** | 인사이트 탭의 데이터 페칭, 차트 필터 기간 변경 이벤트를 처리하고 `insightsRenderer`에 전달합니다. |
| **`modals.js`** | **Modal Controller** | 사용자 설정(이름 매핑 등), 업데이트 소식, 내보내기 등 모달 창 내부의 로직과 이벤트를 제어합니다. |
| **`archive.js`** | **Archive Controller** | 보관함 탭의 페이징, 다중 선택(Bulk), 영구 삭제 및 복원 로직을 캡슐화한 모듈입니다. |
| **`icons.js`** | **SVG Assets** | JS에서 동적으로 주입하는 SVG 아이콘 스트링 모음입니다. |

### 🌍 다국어 지원 (i18n)
| 파일명 | 역할 | 상세 설명 |
| :--- | :--- | :--- |
| **`i18n.js`** | **DOM Translator** | `data-i18n` 속성을 가진 DOM 요소를 찾아 텍스트를 현재 설정된 언어로 교체합니다. |
| **`locales.js`** | **Dictionary** | 한국어(ko), 영어(en), 인니어(id), 태국어(th) 번역 데이터를 담고 있는 정적 딕셔너리입니다. |

---

## 3. 핵심 아키텍처 흐름 (Data Flow)

프론트엔드는 단방향 데이터 흐름을 지향합니다.

### 예시: 업무 완료(Done) 처리 플로우
1. **사용자 액션**: `renderer.js`에 바인딩된 카드 컨테이너(Grid)에서 '완료' 버튼 클릭 (이벤트 위임)
2. **핸들러 호출**: `app.js`에 정의된 `handlers.onToggleDone` 실행
3. **API 통신**: `api.toggleDone(id)`를 호출하여 서버 상태 업데이트
4. **상태 업데이트**: 응답으로 받은 새로운 사용자 통계를 `state.updateStats()`를 통해 전역 상태에 반영
5. **이벤트 전파**: `events.emit(EVENTS.TASK_COMPLETED)` 발생
6. **UI 반응**: `app.js` 내의 구독자(Subscriber)가 이벤트를 감지하여, 폭죽 애니메이션(`renderer.triggerConfetti()`) 및 토스트 알림을 비동기적으로 실행

---

## 4. CSS 아키텍처 (V2 글래스모피즘)

거대한 `style.css`를 컴포넌트 단위로 분리하여 유지보수성을 극대화했습니다.

* **`variables.css`**: 전역 컬러 팔레트, 여백(Spacing) 토큰, **다크/라이트 테마 오버라이드** 변수 정의
* **`base.css`**: Reset CSS 및 Body 기본 배경, 글로벌 타이포그래피
* **`layout.css`**: 컨테이너, 헤더, 그리드 시스템, Empty State 구조 레이아웃
* **`v2-nav.css`**: 상단 네비게이션 탭 및 유틸리티 버튼(아이콘 그룹)
* **`v2-components.css`**: 재사용 가능한 범용 UI (버튼 `.c-btn`, 입력창 `.c-input`, 업무 카드 `.c-task-card`, 탭 `.c-tabs` 등)
* **`v2-modals.css`**: 모달창(`c-modal`) 구조 및 애니메이션, 마크다운 렌더링 스타일
* **`v2-settings.css`**: 설정창 사이드바, 리스트 아이템 디자인
* **`v2-insights.css`**: 통계 탭의 히트맵 그리드, 차트 툴팁, 업적 카드 디자인
* **`badges.css`**: 상태 배지, 태그 배지(.waiting-tag 등) 스타일

> **라이트 테마 대응 (Light Theme)**: 모든 CSS 파일 하단에 `body.light-theme .클래스명` 형태의 오버라이드 룰이 포함되어 있어 반투명 글래스모피즘 효과가 라이트 테마에서도 선명하게 보이도록 보정됩니다.