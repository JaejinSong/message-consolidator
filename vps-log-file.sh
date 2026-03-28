#!/bin/bash

# Why: Simplifies the process of streaming application logs from the remote VPS.
# Instead of manually SSHing and finding the dynamic Docker container ID,
# this script automates the connection and log tailing.

echo ">>> Streaming logs/app.log from the remote VPS container..."

gcloud compute ssh chat-analyzer-vps \
  --zone=us-central1-a \
  --project=gemini-enterprise-487906 \
  --command="sudo docker exec \$(sudo docker ps -q -f name=message-consolidator_app_1) tail -f logs/app.log"
