#!/bin/bash

# VPS 애플리케이션 로그를 tail -f로 확인하는 스크립트
# Usage: ./vps-logs.sh [arguments for docker logs]
# Default: tail -f (follow)

ARGS="${@:- -f}"

echo ">>> VPS(chat-analyzer-vps)의 애플리케이션 로그를 확인합니다. (Args: $ARGS)"

gcloud compute ssh chat-analyzer-vps \
  --zone=us-central1-a \
  --project=gemini-enterprise-487906 \
  --command="docker logs $ARGS message-consolidator-backend"
