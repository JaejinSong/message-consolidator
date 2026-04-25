# WhaTap Builder Image for Go Backend
FROM golang:1.26-alpine

# 1. Install necessary build tools
RUN apk add --no-cache git wget tar upx

# 2. Download and Extract WhaTap agent (stable layer)
# This installs the agent to /usr/whatap/agent/
RUN wget -q https://s3.ap-northeast-2.amazonaws.com/repo.whatap.io/alpine/x86_64/whatap-agent.tar.gz && \
    tar -xzf whatap-agent.tar.gz -C / && \
    rm whatap-agent.tar.gz

ENV GOTOOLCHAIN=auto

# Metadata for identification
LABEL version="0.5.4-builder"
LABEL description="Go Backend Builder with WhaTap Agent (manual instrumentation only)"
