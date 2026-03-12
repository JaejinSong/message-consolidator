# Message Consolidator 🚀

**Message Consolidator**는 Slack과 WhatsApp에서 흩어져 있는 업무 요청사항 및 메시지를 AI(Google Gemini)를 통해 분석하여 하나의 대시보드에서 통합 관리할 수 있게 해주는 도구입니다.

---

## 🌟 주요 기능

- **메시지 통합 수집**: Slack 채널 및 WhatsApp 그룹 메시지를 실시간/정기적으로 스캔합니다.
- **AI 업무 분석**: Google Gemini Pro를 사용하여 메시지 본문에서 작업(Task), 요청자(Requester), 담당자(Assignee), 기한 등을 자동으로 추출합니다.
- **웹 대시보드**: 깔끔한 인터페이스를 통해 업무 현황을 한눈에 파악하고 '완료' 처리를 할 수 있습니다.
- **아카이브 및 내보내기**: 완료된 업무를 7일간 보관하며, CSV 파일로 내보낼 수 있습니다.
- **다국어 번역**: 수집된 업무 내용을 실시간으로 원하는 언어(한국어 등)로 번역할 수 있습니다.
- **메시지 원본 링크**: 분석된 작업에서 원본 메시지(Slack 등)로 바로 이동할 수 있는 링크를 제공합니다.

---

## 🛠 Tech Stack

- **Backend**: Go (Golang)
- **Database**: PostgreSQL (Neon Serverless DB)
- **AI Engine**: Google Gemini Pro (Generative AI)
- **Frontend**: Vanilla JS, HTML, CSS (Responsive Design)
- **Auth**: Google OAuth 2.0
- **Deployment**: Linux VPS, Systemd

---

## ⚙️ 설정 가이드 (Environment Variables)

프로젝트 루트에 `.env` 파일을 생성하고 다음 정보를 설정합니다.

```env
SLACK_TOKEN=xoxb-...          # Slack Bot Token
SLACK_CHANNEL_ID=C...         # 스캔할 기본 Slack 채널 ID
GEMINI_API_KEY=AIza...        # Google Gemini API Key
DATABASE_URL=postgres://...   # Neon 가상 DB 연결 URL
GOOGLE_CLIENT_ID=             # Google OAuth Client ID (선택)
GOOGLE_CLIENT_SECRET=         # Google OAuth Client Secret (선택)
AUTH_SECRET=                  # 세션 암호화용 비밀키
AUTH_DISABLED=true            # 로컬 테스트 시 인증 비활성화 여부
APP_BASE_URL=http://localhost:8080 # 앱 베이스 URL (OAuth 리다이렉트용)
```

---

## 🚀 VPS 배포 가이드

### 1. 바이너리 빌드
```bash
make build
```

### 2. Systemd 서비스 등록
바이너리를 백그라운드에서 안정적으로 실행하기 위해 `systemd`를 사용합니다. 제공된 `message-consolidator.service` 파일을 수정 후 설치합니다.

```bash
# Makefile을 이용한 자동 설치
make install-service
```

구성 예시 (`message-consolidator.service`):
```ini
[Service]
ExecStart=/home/jinro/.gemini/message-consolidator/message-consolidator
WorkingDirectory=/home/jinro/.gemini/message-consolidator
Restart=always
...
```

### 3. WhatsApp 인증
서버 실행 후 대시보드(`http://<vps-ip>:8080`)에 접속하여 'WhatsApp QR' 메뉴를 통해 기기를 연동합니다.

---

## 📁 프로젝트 구조

- `main.go`: 서버 엔트리포인트 및 API 핸들러
- `slack.go` / `whatsapp.go`: 각 플랫폼 메시지 스캔 로직
- `gemini.go`: AI 분석 및 번역 로직 (Google Generative AI)
- `store.go`: DB 저장 및 아카이브 관리
- `static/`: 프론트엔드 정적 파일 (index.html, app.js, style.css)
- `Makefile`: 빌드 및 서비스 관리 자동화

---

## 📝 라이선스

© 2026 Jaejin Song. All rights reserved.
