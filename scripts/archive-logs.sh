#!/bin/bash
# Cold archive: compress logs older than 30 days and upload to GCS.
# Run via VM host cron at 03:00 daily (prime hour avoids scheduled job contention):
#   0 3 * * * /home/jjsong/message-consolidator/scripts/archive-logs.sh >> /var/log/mc-archive.log 2>&1
#
# Prerequisites:
#   - gcloud authenticated with roles/storage.objectCreator on GCS_BUCKET
#   - LOG_DIR and GCS_BUCKET set, or use defaults below
set -euo pipefail

LOG_DIR="${LOG_DIR:-$HOME/message-consolidator/logs}"
GCS_BUCKET="${GCS_BUCKET:-}"
RETENTION_DAYS=30

if [ -z "$GCS_BUCKET" ]; then
    echo "[archive-logs] ERROR: GCS_BUCKET env not set" >&2
    exit 1
fi

cd "$LOG_DIR" 2>/dev/null || { echo "[archive-logs] LOG_DIR $LOG_DIR not found — skip"; exit 0; }

archived=0
failed=0

while IFS= read -r -d '' logfile; do
    gz="${logfile}.gz"
    gcs_path="gs://${GCS_BUCKET}/message-consolidator/${logfile#./}"

    # compress
    if ! gzip -k "$logfile" -c > "$gz" 2>/dev/null; then
        echo "[archive-logs] WARN: gzip failed for $logfile"
        rm -f "$gz"
        ((failed++)) || true
        continue
    fi

    # verify integrity
    local_sha=$(sha256sum "$gz" | awk '{print $1}')
    if ! gcloud storage cp "$gz" "${gcs_path}.gz" --quiet 2>/dev/null; then
        echo "[archive-logs] WARN: GCS upload failed for $logfile"
        rm -f "$gz"
        ((failed++)) || true
        continue
    fi

    # verify uploaded sha matches
    remote_sha=$(gcloud storage hash "${gcs_path}.gz" --format='value(digest)' 2>/dev/null || true)
    if [ -n "$remote_sha" ] && [ "$local_sha" != "$remote_sha" ]; then
        echo "[archive-logs] WARN: sha mismatch for $logfile — keeping local copy"
        rm -f "$gz"
        ((failed++)) || true
        continue
    fi

    rm -f "$gz" "$logfile"
    echo "[archive-logs] archived: $logfile → ${gcs_path}.gz"
    ((archived++)) || true

done < <(find . -name "*.jsonl" -mtime +${RETENTION_DAYS} -print0 2>/dev/null)

echo "[archive-logs] done: archived=${archived} failed=${failed}"
