#!/bin/bash

# Cloud Run Migration Deployment Script
# This script builds the Docker image and deploys it to Google Cloud Run.

set -e

# Configuration (Edit these or set as env vars)
PROJECT_ID=${PROJECT_ID:-$(gcloud config get-value project)}
REGION=${REGION:-"asia-northeast3"}
SERVICE_NAME=${SERVICE_NAME:-"message-consolidator"}
IMAGE_TAG="gcr.io/$PROJECT_ID/$SERVICE_NAME:latest"

if [ -z "$PROJECT_ID" ]; then
    echo "Error: PROJECT_ID is not set. Please run 'gcloud config set project [PROJECT_ID]'"
    exit 1
fi

echo "--- Building and Pushing Image ---"
gcloud builds submit --tag $IMAGE_TAG .

echo "--- Deploying to Cloud Run ---"
# Note: You should set other environment variables (SLACK_TOKEN, GEMINI_API_KEY, etc.)
# in the Cloud Run console or via --set-env-vars here.
gcloud run deploy $SERVICE_NAME \
  --image $IMAGE_TAG \
  --platform managed \
  --region $REGION \
  --allow-unauthenticated \
  --env-vars-file env.yaml

echo "--- Deployment Detailed Information ---"
gcloud run services describe $SERVICE_NAME --region $REGION --format='value(status.url)'
