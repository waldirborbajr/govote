#!/usr/bin/env bash
# Cria uma enquete ativa de Black Friday para os testes de stress.
# Uso: ./scripts/setup-poll.sh [BASE_URL]
set -euo pipefail

BASE_URL="${1:-http://localhost:9080}"

echo "==> Health check em ${BASE_URL}/health"
curl -sf "${BASE_URL}/health" | head -c 200
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
  }')

echo "${RESP}" | head -c 800
echo

# Extrai o ID da enquete (requer jq; fallback manual)
if command -v jq >/dev/null 2>&1; then
  POLL_ID=$(echo "${RESP}" | jq -r '.id // empty')
  if [[ -n "${POLL_ID}" ]]; then
    echo ""
    echo "✅ Enquete criada com sucesso. POLL_ID=${POLL_ID}"
    echo "   Exporte antes de rodar o k6:"
    echo "   export POLL_ID=${POLL_ID}"
    echo "   ou edite .env.stress / docker-compose"
  else
    echo "⚠️  Não foi possível extrair o ID automaticamente. Verifique a resposta acima."
  fi
else
  echo ""
  echo "ℹ️  jq não encontrado. Copie manualmente o campo \"id\" da resposta e use:"
  echo "   export POLL_ID=<id>"
fi

echo ""
echo "==> Listando polls ativas:"
curl -sf "${BASE_URL}/polls" | head -c 600
echo
