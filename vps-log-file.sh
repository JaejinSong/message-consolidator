#!/bin/bash

# VPS 애플리케이션 '내부'의 로그 파일(logs/app.log)을 tail -f로 확인하는 스크립트
# Docker 컨테이너 내부에 쌓이는 파일을 실시간으로 스트리밍합니다.

echo ">>> VPS(chat-analyzer-vps) 컨테이너 내부의 logs/app.log 를 확인합니다..."

gcloud compute ssh chat-analyzer-vps \
  --zone=us-central1-a \
  --project=gemini-enterprise-487906 \
  --command="sudo docker exec \$(sudo docker ps -q -f name=message-consolidator_app_1) tail -f logs/app.log"
