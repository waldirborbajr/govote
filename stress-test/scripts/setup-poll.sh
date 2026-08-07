#!/usr/bin/env bash
set -euo pipefail
BASE_URL="${1:-http://localhost:8080}"

echo "==> Health ${BASE_URL}/health"
curl -sf "${BASE_URL}/health" || curl -sf "${BASE_URL}/health"
echo

echo "==> Criando enquete Black Friday..."
RESP=$(curl -sf -X POST "${BASE_URL}/polls" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Black Friday 2026 - Qual produto você mais quer?",
    "type": "radio",
    "start_date": "2025-01-01T00:00:00Z",
    "end_date": "2027-12-31T23:59:59Z",
    "answers": [
      { "text": "Smartphone" },
      { "text": "Notebook" },
      { "text": "TV 4K" },
      { "text": "Fone de ouvido" },
      { "text": "Console" }
    ]
  }' || true)

echo "${RESP}" | head -c 800
echo

if command -v jq >/dev/null 2>&1; then
  POLL_ID=$(echo "${RESP}" | jq -r '.id // empty')
  if [[ -n "${POLL_ID}" && "${POLL_ID}" != "null" ]]; then
    echo "✅ POLL_ID=${POLL_ID}"
    echo "export POLL_ID=${POLL_ID}"
  fi
fi

echo "==> Polls ativas:"
curl -sf "${BASE_URL}/polls" | head -c 600
echo
