#!/usr/bin/env bash
# Simula queda de um nó da API e posterior recuperação sob carga.
# Uso:
#   ./chaos/kill-node.sh govote-2 90
#   (mata govote-2, espera 90s, sobe de novo)
set -euo pipefail

NODE="${1:-govote-2}"
DOWN_SECS="${2:-90}"

echo "[chaos] $(date -Is) STOP ${NODE}"
docker stop "${NODE}" || docker kill "${NODE}" || true

echo "[chaos] nó ${NODE} fora por ${DOWN_SECS}s — observe k6 / nginx / stats"
sleep "${DOWN_SECS}"

echo "[chaos] $(date -Is) START ${NODE}"
docker start "${NODE}"

echo "[chaos] aguardando health..."
for i in $(seq 1 30); do
  if docker exec "${NODE}" wget -q --spider http://127.0.0.1:9080/health 2>/dev/null; then
    echo "[chaos] ${NODE} healthy"
    exit 0
  fi
  sleep 2
done
echo "[chaos] aviso: health não confirmado a tempo"
exit 1
