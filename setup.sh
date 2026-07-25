#!/usr/bin/env bash
#
# setup.sh — prepara um deploy novo (do zero) do govote nesta VPS:
#   1. Gera GOVOTE_JWT_SECRET e GOVOTE_CPF_PEPPER aleatórios e grava no
#      docker-compose.yaml (substitui o placeholder "changeme...").
#   2. Cria ./data e ajusta dono para o UID/GID 65532 (usuário não-root
#      que o container usa — ver Dockerfile).
#
# Uso:
#   ./setup.sh
# 
#   docker compose up -d
#
# Rode este script sempre que recriar o ambiente do zero (nova VPS, ou
# ./data apagado). Se ./data ou os secrets já existirem, o script pede
# confirmação antes de sobrescrever — regenerar os secrets invalida
# sessões/tokens já emitidos.

set -euo pipefail

COMPOSE_FILE="${1:-docker-compose.yaml}"
DATA_DIR="./data"
CONTAINER_UID=65532
CONTAINER_GID=65532

# --- checagens básicas -------------------------------------------------

if ! command -v openssl >/dev/null 2>&1; then
  echo "❌ openssl não encontrado. Instale com: apt-get install -y openssl" >&2
  exit 1
fi

if [ ! -f "$COMPOSE_FILE" ]; then
  echo "❌ $COMPOSE_FILE não encontrado no diretório atual ($(pwd))." >&2
  exit 1
fi

if [ "$(id -u)" -ne 0 ]; then
  echo "⚠️  Rodando sem root — o chown para UID $CONTAINER_UID pode falhar" \
       "dependendo das permissões atuais. Rode com sudo se necessário." >&2
fi

# --- passo 1: secrets ----------------------------------------------------

already_configured=false
if ! grep -q "GOVOTE_JWT_SECRET=changeme" "$COMPOSE_FILE" 2>/dev/null; then
  already_configured=true
fi

if [ "$already_configured" = true ]; then
  read -r -p "⚠️  $COMPOSE_FILE já parece ter secrets configurados. Gerar novos e sobrescrever (invalida sessões ativas)? [y/N] " reply
  if [[ ! "$reply" =~ ^[Yy]$ ]]; then
    echo "↷  Mantendo secrets atuais."
  else
    already_configured=false
  fi
fi

if [ "$already_configured" = false ]; then
  cp "$COMPOSE_FILE" "${COMPOSE_FILE}.bak.$(date +%s)"
  echo "📦 Backup salvo antes de editar."

  JWT_SECRET="$(openssl rand -hex 32)"
  CPF_PEPPER="$(openssl rand -hex 32)"

  sed -i.tmp \
    -e "s|GOVOTE_JWT_SECRET=.*|GOVOTE_JWT_SECRET=${JWT_SECRET}|" \
    -e "s|GOVOTE_CPF_PEPPER=.*|GOVOTE_CPF_PEPPER=${CPF_PEPPER}|" \
    "$COMPOSE_FILE"
  rm -f "${COMPOSE_FILE}.tmp"

  echo "🔐 GOVOTE_JWT_SECRET e GOVOTE_CPF_PEPPER gerados e gravados em $COMPOSE_FILE."
fi

# --- passo 2: diretório de dados -----------------------------------------

if [ -d "$DATA_DIR" ] && [ -n "$(ls -A "$DATA_DIR" 2>/dev/null)" ]; then
  read -r -p "⚠️  $DATA_DIR já existe e não está vazio. Continuar sem apagar? [Y/n] " reply
  if [[ "$reply" =~ ^[Nn]$ ]]; then
    echo "Abortado. Nada foi apagado — resolva manualmente e rode de novo." >&2
    exit 1
  fi
fi

mkdir -p "$DATA_DIR"
chown -R "${CONTAINER_UID}:${CONTAINER_GID}" "$DATA_DIR"
chmod 700 "$DATA_DIR"

echo "📁 $DATA_DIR pronto (dono ${CONTAINER_UID}:${CONTAINER_GID})."
echo ""
echo "✅ Setup concluído. Suba com:"
echo "   docker compose up -d"
