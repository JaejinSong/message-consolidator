# Cloud Run Migration Guide

이 문서는 기존 VPS 기반 아키텍처에서 Google Cloud Run 서버리스 아키텍처로 전환하면서 변경된 사항과 주요 특징을 설명합니다.

## 아키텍처 비교

| 항목 | 기존 VPS 방식 | Cloud Run 방식 (Migration) |
| :--- | :--- | :--- |
| **실행 모델** | 상시 가동 (Always-on) | 요청 시 가동 (On-demand / Scale-to-zero) |
| **스캔 트리거** | 내부 백그라운드 고루틴 (59초 주기) | 외부 Cloud Scheduler 호출 (`/api/internal/scan`) |
| **WhatsApp** | 상시 연결 유지, 메모리 버퍼링 | 호출 시 재연결 및 동기화 (Trigger-based) |
| **포트 설정** | 고정 포트 (8080) | 동적 포트 (`$PORT` 환경 변수 사용) |
| **공진 방지** | 59초 고정 주기 사용 | 1분 주기 + 0~5초 랜덤 Jitter 추가 |

## 주요 설정 (Environment Variables)

Cloud Run 배포 시 다음 환경 변수를 필수로 설정해야 합니다:

- `CLOUD_RUN_MODE`: `true`로 설정 (백그라운드 스캐너 비활성화 여부 결정)
- `INTERNAL_SCAN_SECRET`: Cloud Scheduler와의 인증을 위한 보안 토큰
- `DATABASE_URL`: Neon PostgreSQL 연결 문자열
- `GEMINI_API_KEY`: 분석을 위한 Gemini API 키
- `SLACK_TOKEN`: Slack 통합용 토큰

## Cloud Run 최적화 사항

### 1. 지터(Jitter) 로직
Cloud Scheduler는 1분 단위로 정확히 호출하려 하지만, 여러 인스턴스가 동시에 같은 자원(DB 등)에 접근하는 것을 방지하기 위해 `CLOUD_RUN_MODE`에서는 실행 직전 **0~5초의 랜덤한 대기 시간**을 갖습니다.

### 2. WhatsApp 처리 로직
Cloud Run 인스턴스는 유휴 상태일 때 종료되므로 WhatsApp 연결이 끊어집니다. `/api/internal/scan` 호출 시 `InitWhatsApp`이 실행되어 다시 연결을 맺고, 그동안 쌓인 메시지를 가져와 처리한 후 응답을 반환하도록 설계되었습니다.

## 배포 및 설정 스크립트
- `cloud-run-deploy.sh`: 이미지 빌드 및 Cloud Run 서비스 배포
- `cloud-scheduler-setup.sh`: 1분 주기 API 호출 스케줄러 설정

---
*주의: WhatsApp 세션 유지를 위해서는 Neon DB에 세션 정보가 안정적으로 저장되어 있어야 합니다.*
