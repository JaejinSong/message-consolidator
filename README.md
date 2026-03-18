# Message Consolidator 🚀

**Message Consolidator**는 Slack과 WhatsApp에서 흩어져 있는 업무 요청사항 및 메시지를 AI(Google Gemini)를 통해 분석하여 하나의 대시보드에서 통합 관리할 수 있게 해주는 도구입니다.

---

## 🌟 주요 기능

- **메시지 통합 수집**: Slack 채널, WhatsApp 그룹 메시지 및 **Gmail 이메일**을 정기적으로 스캔합니다.
- **AI 업무 분석**: Google Gemini Pro를 사용하여 메시지 본문 및 이메일 내용에서 작업(Task), 요청자(Requester), 담당자(Assignee), 기한 등을 자동으로 추출합니다.
- **다중 사용자 지원**: Google 로그인(OAuth 2.0)을 통해 개별 사용자별로 전용 대시보드와 WhatsApp 및 Gmail 연동을 지원합니다.
- **간편한 연동 UX**: 대시보드 헤더의 서비스 아이콘을 클릭하여 WhatsApp(QR 코드)이나 Gmail(OAuth)을 즉시 연결할 수 있습니다.
- **탭 기반 웹 대시보드**: 'My Tasks'와 'Other Tasks' 탭을 통해 업무를 효율적으로 분류하여 관리할 수 있습니다.
- **프로필 연동**: Google 프로필 사진을 대시보드에서 바로 확인할 수 있습니다.
- **소프트 삭제 (Soft Delete)**: 업무를 삭제하면 DB에서 완전히 지워지지 않고 '아카이브'로 이동하여 나중에 확인할 수 있습니다.
- **아카이브 및 내보내기**: 완료되거나 삭제된 업무를 관리하며, CSV 파일로 내보낼 수 있습니다.
- **다국어 번역**: 수집된 업무 내용을 실시간으로 원하는 언어(한국어 등)로 번역할 수 있습니다.
- **로그 자동 관리**: Lumberjack을 사용하여 일간 로그 로테이션 및 7일 보관 기능을 지원합니다.
- **데이터베이스 최적화**: Neon DB의 sleep 모드를 활용하기 위해 인메모리 캐싱 및 연결 풀 최적화가 적용되어 있습니다.

---

## 🛠 Tech Stack

- **Backend**: Go (Golang)
- **Database**: PostgreSQL (Neon Serverless DB)
- **AI Engine**: Google Gemini Pro (Generative AI)
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
upx --best message-consolidator-vps
```

### 2. Docker 실행
```bash
docker-compose up -d --build
```

### 💡 Build Speed & Size Tips
- **Binary Size Optimization**: Build with `-ldflags="-s -w"` to strip symbols and debug info.
- **Binary Compression**: Use `upx --best` to further compress the binary (37MB -> ~10MB).
- **Keep CGO Disabled**: Use `CGO_ENABLED=0` for faster, statically linked binaries unless CGO is strictly required.
- **GOCACHE on RAM Disk**: Since `jjsong-devmachine` has 32GB RAM, use `tmpfs` to speed up I/O.
  ```bash
  sudo mount -t tmpfs -o size=2G tmpfs /home/jinro/.cache/go-build
  ```

---

## 📁 프로젝트 구조

- `main.go`: API 라우팅 및 핸들러
- `auth.go`: Google OAuth 및 세션 관리
- `store.go`: PostgreSQL 연동 및 캐싱 로직
- `whatsapp.go`: 다중 사용자별 WhatsApp 연동 및 스캔
- `slack.go`: Slack 메시지 수집 로직
- `gemini.go`: AI 분석 및 번역 (Google Generative AI)
- `gmail.go`: Gmail API 연동 및 이메일 본문 분석 로직
- `static/`: 프론트엔드 (index.html, app.js, style.css)
- `docker-compose.yml` / `Dockerfile`: 컨테이너라이제이션 설정

---

## 📝 라이선스

© 2026 Jaejin Song. All rights reserved.

