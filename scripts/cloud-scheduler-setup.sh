#!/bin/bash

# Cloud Scheduler Setup for message-consolidator
# This script creates a Cloud Scheduler job to trigger the internal scan API every minute.

set -e

# Configuration
PROJECT_ID=${PROJECT_ID:-$(gcloud config get-value project)}
REGION=${REGION:-"asia-northeast3"}
SERVICE_NAME=${SERVICE_NAME:-"message-consolidator"}
JOB_NAME="${SERVICE_NAME}-scan-job"
INTERNAL_SCAN_SECRET=${INTERNAL_SCAN_SECRET:-"my-cloudrun-secret-2026"}

if [ -z "$PROJECT_ID" ]; then
    echo "Error: PROJECT_ID is not set."
    exit 1
fi

# Get the Cloud Run URL
SERVICE_URL=$(gcloud run services describe $SERVICE_NAME --region $REGION --format='value(status.url)')

if [ -z "$SERVICE_URL" ]; then
    echo "Error: Could not find service URL for $SERVICE_NAME in $REGION. Is it deployed?"
    exit 1
fi

SCAN_URL="${SERVICE_URL}/api/internal/scan"

echo "--- Creating/Updating Cloud Scheduler Job [$JOB_NAME] ---"
# Check if job exists, if so delete it first (simplest way to update)
if gcloud scheduler jobs describe $JOB_NAME --location $REGION &>/dev/null; then
    echo "Found existing job, deleting..."
    gcloud scheduler jobs delete $JOB_NAME --location $REGION --quiet
fi

gcloud scheduler jobs create http $JOB_NAME \
  --location $REGION \
  --schedule "* * * * *" \
  --uri $SCAN_URL \
  --http-method GET \
  --headers "X-Internal-Secret=$INTERNAL_SCAN_SECRET" \
  --time-zone "Asia/Seoul"

echo "--- Cloud Scheduler Job Created ---"
echo "Target URL: $SCAN_URL"
echo "Schedule: Every minute (* * * * *)"
