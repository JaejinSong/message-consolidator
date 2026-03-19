# Message Consolidator 🚀

**Message Consolidator**는 Slack과 WhatsApp에서 흩어져 있는 업무 요청사항 및 메시지를 AI(Google Gemini)를 통해 분석하여 하나의 대시보드에서 통합 관리할 수 있게 해주는 도구입니다.

---

## 🌟 주요 기능

- **메시지 통합 수집**: Slack 채널, WhatsApp 그룹 메시지 및 **Gmail 이메일**을 정기적으로 스캔합니다.
- **AI 업무 분석**: Google Gemini 3 Flash Preview를 사용하여 메시지 본문 및 이메일 내용에서 작업(Task), 요청자(Requester), 담당자(Assignee), 기한 등을 자동으로 추출합니다.
- **다중 사용자 지원**: Google 로그인(OAuth 2.0)을 통해 개별 사용자별로 전용 대시보드와 WhatsApp 및 Gmail 연동을 지원합니다.
- **간편한 연동 UX**: 대시보드 헤더의 서비스 아이콘을 클릭하여 WhatsApp(QR 코드)이나 Gmail(OAuth)을 즉시 연결할 수 있습니다.
- **탭 기반 웹 대시보드**: 'My Tasks'와 'Other Tasks' 탭을 통해 업무를 효율적으로 분류하여 관리할 수 있습니다.
- **프로필 연동**: Google 프로필 사진을 대시보드에서 바로 확인할 수 있습니다.
- **소프트 삭제 (Soft Delete)**: 업무를 삭제하면 DB에서 완전히 지워지지 않고 '아카이브'로 이동하여 나중에 확인할 수 있습니다.
- **자동 아카이브 (Auto-Archive)**: 생성된 지 7일이 지난 오래된 업무는 데이터베이스 최적화 스케줄링을 통해 자동으로 아카이브되어 대시보드를 깔끔하게 유지합니다.
- **아카이브 및 고성능 검색**: 완료되거나 삭제된 업무를 관리하며, **GIN 트리그램 인덱스** 및 **복합 인덱스(Compound Indexes)** 기반의 고성능 검색과 서버 사이드 정렬, 페이징 처리를 통해 대량의 데이터를 효율적으로 조회할 수 있습니다.
- **실시간 로딩 인디케이터**: Archive 데이터를 불러오거나 정렬할 때 사용자에게 즉각적인 시각적 피드백을 제공합니다.
- **스마트 내보내기 (Export)**: 필터링된 검색 결과를 CSV 또는 **Excel(.xlsx)** 형식으로 내보낼 수 있으며, 실행 전 요약 정보를 제공하는 모달이 추가되었습니다.
- **다국어 번역**: 수집된 업무 내용을 실시간으로 원하는 언어(한국어 등)로 번역할 수 있습니다.
- **로그 자동 관리**: Lumberjack을 사용하여 일간 로그 로테이션 및 7일 보관 기능을 지원합니다.
- **데이터베이스 최적화**: Neon DB의 sleep 모드를 활용하기 위해 인메모리 캐싱 및 연결 풀 최적화가 적용되어 있으며, **번역 캐싱** 기능을 통해 실시간 언어 전환 성능이 극대화되었습니다.

---

## 🛠 Tech Stack

- **Backend**: Go (Golang)
- **Database**: PostgreSQL (Neon Serverless DB)
- **AI Engine**: Google Gemini 3 Flash Preview (Generative AI)
- **Frontend**: Vanilla JS, HTML, CSS (Tabs Layout)
- **Container**: Docker, Docker Compose
- **Auth**: Google OAuth 2.0 (with Profile Picture)

---

## ⚙️ 설정 가이드 (Environment Variables)

프로젝트 루트의 `.env` 파일 또는 Docker Compose 환경 변수를 통해 설정합니다.

```env
SLACK_TOKEN=xoxb-...          # Slack Bot Token
SLACK_CHANNEL_ID=C...         # 스캔할 기본 Slack 채널 ID (선택)
GEMINI_API_KEY=AIza...        # Google Gemini API Key
DATABASE_URL=postgres://...   # Neon DB 연결 URL
GOOGLE_CLIENT_ID=...          # Google OAuth Client ID
GOOGLE_CLIENT_SECRET=...      # Google OAuth Client Secret
AUTH_SECRET=...               # 세션 암호화용 비밀키
APP_BASE_URL=https://...      # 앱 베이스 URL
LOG_LEVEL=INFO                # 로그 레벨 (DEBUG, INFO, WARN, ERROR)
```

---

## 🚀 배포 가이드 (Docker)

Docker Compose를 사용하여 간편하게 배포할 수 있습니다.

### 1. 바이너리 빌드 (최적화 적용)
```bash
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o message-consolidator-vps .
upx -1 message-consolidator-vps
```

### 2. Docker 실행
```bash
docker-compose up -d --build
```

### 💡 Build Speed & Size Tips
- **Binary Size Optimization**: Build with `-ldflags="-s -w"` to strip symbols and debug info.
- **Binary Compression**: Use `upx -1` to optimize build speed while still reducing binary size.
- **Keep CGO Disabled**: Use `CGO_ENABLED=0` for faster, statically linked binaries unless CGO is strictly required.
- **GOCACHE on RAM Disk**: Since `jjsong-devmachine` has 32GB RAM, use `tmpfs` to speed up I/O.
  ```bash
  sudo mount -t tmpfs -o size=2G tmpfs /home/jinro/.cache/go-build
  ```

---

## 📁 프로젝트 구조

- `main.go`: 서버 초기화 및 라우팅 설정
- `handlers.go`: 모든 HTTP 핸들러 (API 엔드포인트)
- `scanner.go`: 각 서비스별 메시지 스캔 및 비즈니스 로직
- `logger.go`: 레벨별 로깅 시스템 구현
- `types.go`: 공통 데이터 구조 및 상수 정의
- `auth.go`: Google OAuth 및 세션 관리
- `store.go`: PostgreSQL 연동 및 번역 캐싱 로직
- `whatsapp.go`: WhatsApp 연동 및 세션 관리
- `slack.go`: Slack 메시지 수집 로직
- `gemini.go`: AI 분석 및 번역 (System Instruction 최적화)
- `gmail.go`: Gmail API 연동 및 이메일 연동
- `static/`: 프론트엔드 (index.html, app.js, style.css)
- `docker-compose.yml` / `Dockerfile`: 컨테이너라이제이션 설정

---

## 📝 라이선스

© 2026 Jaejin Song. All rights reserved.

