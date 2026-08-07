#!/usr/bin/env bash
# Loop de caos: alterna queda de govote-1/2/3 enquanto o stress roda.
# Uso (em paralelo ao k6):
#   ./chaos/chaos-loop.sh 3 60
#   (3 ciclos, cada nó fica 60s down)
set -euo pipefail

CYCLES="${1:-3}"
DOWN_SECS="${2:-60}"
NODES=(govote-1 govote-2 govote-3)

for ((c=1; c<=CYCLES; c++)); do
  NODE="${NODES[$(( (c-1) % ${#NODES[@]} ))]}"
  echo "======== ciclo ${c}/${CYCLES} ========"
  "$(dirname "$0")/kill-node.sh" "${NODE}" "${DOWN_SECS}"
  echo "[chaos] estabilização 30s antes do próximo ciclo"
  sleep 30
done

echo "[chaos] finalizado"
