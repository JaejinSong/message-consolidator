#!/bin/sh
set -e

WHATAP_AGENT="/usr/whatap/agent/whatap-agent"
APP_BIN="./message-consolidator"

# Ensure logs directory exists for WhaTap Logsink
mkdir -p /app/logs

echo "[INFO] Container initialization started."

# 1. Start the WhaTap Data Relay Agent if present and executable
if [ -f "$WHATAP_AGENT" ]; then
    echo "[INFO] Starting WhaTap Data Relay Agent with sh..."
    sh "$WHATAP_AGENT" start
else
    echo "[WARN] WhaTap agent not found at $WHATAP_AGENT. Skipping Relay Agent initialization."
fi

# 2. Execute the main application
echo "[INFO] Starting Application..."
if [ -x "$APP_BIN" ]; then
    exec "$APP_BIN"
else
    echo "[ERROR] Application binary not found or not executable!"
    exit 1
fi
