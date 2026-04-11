#!/bin/bash

# Configuration
PROJECT_ID="gemini-enterprise-487906"
ZONE="us-central1-a"
VPS_NAME="chat-analyzer-vps"

echo "==> Connecting to VPS: ${VPS_NAME}..."
echo "==> Tip: Once connected, run 'cd ~/message-consolidator && sudo docker compose logs -f' to check logs."

gcloud compute ssh ${VPS_NAME} --zone=${ZONE} --project=${PROJECT_ID}
