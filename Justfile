# govote — operações locais, compose normal e Black Friday
# Uso: just <receita>
# Lista: just --list

set shell := ["bash", "-eu", "-o", "pipefail", "-c"]
set dotenv-load := true

# ---------------------------------------------------------------------------
# Variáveis (sobrescreva: just poll_id=2 stress-bf)
# ---------------------------------------------------------------------------

compose       := "docker compose -f docker-compose.yaml"
compose_bf    := "docker compose -f docker-compose.bf.yaml"
compose_stress := "docker compose -f stress-test/docker-compose.stress.yaml"

base_url      := env_var_or_default("BASE_URL", "http://localhost:8080")
base_url_dev  := env_var_or_default("BASE_URL_DEV", "http://localhost:9080")
poll_id       := env_var_or_default("POLL_ID", "1")
node          := env_var_or_default("NODE", "govote-2")
down_secs     := env_var_or_default("DOWN_SECS", "90")
cycles        := env_var_or_default("CYCLES", "3")
bench_pkg     := env_var_or_default("BENCH_PKG", "./...")
bench_time    := env_var_or_default("BENCH_TIME", "3s")
bench_count   := env_var_or_default("BENCH_COUNT", "1")
go_flags      := env_var_or_default("GOFLAGS", "")

# ---------------------------------------------------------------------------
# Ajuda
# ---------------------------------------------------------------------------

# Lista todas as receitas (padrão)
default:
    @just --list

# ---------------------------------------------------------------------------
# Ambiente
# ---------------------------------------------------------------------------

# Copia .env.bf.example → .env se ainda não existir
env:
    #!/usr/bin/env bash
    if [[ ! -f .env ]]; then
      if [[ -f .env.bf.example ]]; then
        cp .env.bf.example .env
        echo "Criado .env a partir de .env.bf.example — revise os secrets."
      else
        echo "Nenhum .env.bf.example encontrado. Crie .env manualmente."
        exit 1
      fi
    else
      echo ".env já existe."
    fi

# ---------------------------------------------------------------------------
# Go — build / test / vet / bench
# ---------------------------------------------------------------------------

# Compila o binário em ./bin/govote
build:
    mkdir -p bin
    go build {{go_flags}} -ldflags="-s -w" -o bin/govote ./cmd/govote
    @echo "bin/govote ok"

# Build com race detector (debug)
build-race:
    mkdir -p bin
    go build -race {{go_flags}} -o bin/govote-race ./cmd/govote
    @echo "bin/govote-race ok"

# go mod tidy + download
deps:
    go mod tidy
    go mod download

# Unit tests
test:
    go test {{go_flags}} ./...

# Tests com race detector
test-race:
    go test -race {{go_flags}} ./...

# Tests verbosos + coverage
test-cover:
    mkdir -p coverage
    go test {{go_flags}} -coverprofile=coverage/coverage.out ./...
    go tool cover -func=coverage/coverage.out | tail -20
    @echo "HTML: go tool cover -html=coverage/coverage.out"

# Vet estático
vet:
    go vet ./...

# golangci-lint (se instalado)
lint:
    #!/usr/bin/env bash
    if command -v golangci-lint >/dev/null 2>&1; then
      golangci-lint run ./...
    else
      echo "golangci-lint não instalado — rodando go vet"
      go vet ./...
    fi

# Benchmarks: just bench
# Pacote/tempo: just bench_pkg=./internal/cache bench_time=5s bench
bench:
    go test {{go_flags}} -run=^$ -bench=. -benchmem -benchtime={{bench_time}} -count={{bench_count}} {{bench_pkg}}

# Benchmarks só de um pacote (atalho)
bench-cache:
    just bench_pkg=./internal/cache bench

bench-poll:
    just bench_pkg=./internal/poll bench

bench-security:
    just bench_pkg=./internal/security bench

# fmt
fmt:
    go fmt ./...

# build + vet + test
check: fmt vet test
    @echo "check ok"

# Pipeline CI local
ci: deps check build
    @echo "ci ok"

# Roda o binário local (sem Docker)
run-local: build
    ./bin/govote

# ---------------------------------------------------------------------------
# Docker — compose NORMAL (single instance, docker-compose.yaml)
# ---------------------------------------------------------------------------

# Build da imagem do compose normal
dev-build:
    {{compose}} build

# Sobe o container normal (govote único, portas 9080/8443)
dev-up: env
    mkdir -p data
    {{compose}} up -d
    @echo ""
    @echo "Compose normal:"
    @echo "  HTTP:  {{base_url_dev}}"
    @echo "  HTTPS: https://localhost:8443"
    @echo "  Health: {{base_url_dev}}/health"

# Sobe com rebuild
dev-up-build: env
    mkdir -p data
    {{compose}} up -d --build
    @echo "HTTP: {{base_url_dev}}  Health: {{base_url_dev}}/health"

# Para o compose normal
dev-down:
    {{compose}} down

# Para e remove volumes nomeados do compose normal
dev-destroy:
    {{compose}} down -v

# Status
dev-ps:
    {{compose}} ps

# Logs
dev-logs:
    {{compose}} logs -f --tail=100

# Restart
dev-restart:
    {{compose}} restart

# Health no compose normal
dev-health:
    curl -sf "{{base_url_dev}}/health" | tee /dev/stderr
    @echo

# Stats do container normal
dev-stats:
    docker stats govote

# Pull da imagem publicada (sem build local)
dev-pull:
    {{compose}} pull

# Recria o container normal
dev-recreate:
    {{compose}} up -d --force-recreate

# Setup enquete no compose normal
dev-setup-poll:
    chmod +x stress-test/scripts/setup-poll.sh
    ./stress-test/scripts/setup-poll.sh "{{base_url_dev}}"

# Smoke k6 contra o compose normal
dev-stress-smoke:
    {{compose_stress}} --profile stress run --rm \
      -e BASE_URL={{base_url_dev}} -e POLL_ID={{poll_id}} \
      k6 run /scripts/smoke.js

# ---------------------------------------------------------------------------
# Docker — stack BLACK FRIDAY
# ---------------------------------------------------------------------------

# Build das imagens do stack BF
bf-build:
    {{compose_bf}} build

# Alias histórico: build = bf-build
build-docker: bf-build

# Sobe Redis + Nginx + 3× API + vote-worker
up: env
    mkdir -p data
    {{compose_bf}} up -d --build
    @echo ""
    @echo "API pública (Nginx): {{base_url}}"
    @echo "Health:              {{base_url}}/health"

# Alias
bf-up: up

# Sobe BF sem rebuild
start:
    {{compose_bf}} up -d

bf-start: start

# Para o stack BF (mantém volumes)
down:
    {{compose_bf}} down

bf-down: down

# Para e remove volumes (Redis)
destroy:
    {{compose_bf}} down -v
    @echo "Volume redis-data removido. data/ no host permanece."

bf-destroy: destroy

# Status BF
ps:
    {{compose_bf}} ps

bf-ps: ps

# Logs BF
logs:
    {{compose_bf}} logs -f --tail=100

bf-logs: logs

# Logs de um serviço: just logs-svc govote-1
logs-svc svc:
    {{compose_bf}} logs -f --tail=200 {{svc}}

# Recursos BF
stats:
    docker stats govote-1 govote-2 govote-3 govote-nginx govote-redis govote-vote-worker

bf-stats: stats

# Health via Nginx
health:
    curl -sf "{{base_url}}/health" | tee /dev/stderr
    @echo

bf-health: health

# Reinicia um nó: just restart-node govote-2
restart-node node=node:
    docker restart {{node}}

# ---------------------------------------------------------------------------
# Dados de teste (BF / Nginx por padrão)
# ---------------------------------------------------------------------------

# Cria enquete Black Friday
setup-poll:
    chmod +x stress-test/scripts/setup-poll.sh
    ./stress-test/scripts/setup-poll.sh "{{base_url}}"

# Lista polls
polls:
    curl -sf "{{base_url}}/polls" | head -c 2000
    @echo

# ---------------------------------------------------------------------------
# Stress (k6) — alvo Nginx BF por padrão
# ---------------------------------------------------------------------------

stress-smoke:
    {{compose_stress}} --profile stress run --rm \
      -e BASE_URL={{base_url}} -e POLL_ID={{poll_id}} \
      k6 run /scripts/smoke.js

stress-bf:
    {{compose_stress}} --profile stress run --rm \
      -e BASE_URL={{base_url}} -e POLL_ID={{poll_id}} \
      k6 run /scripts/blackfriday.js \
      --out json=/results/bf-$(date +%Y%m%d-%H%M).json

stress-burst:
    {{compose_stress}} --profile stress run --rm \
      -e BASE_URL={{base_url}} -e POLL_ID={{poll_id}} \
      k6 run /scripts/vote-burst.js \
      --out json=/results/burst-$(date +%Y%m%d-%H%M).json

stress-auth:
    {{compose_stress}} --profile stress run --rm \
      -e BASE_URL={{base_url}} \
      k6 run /scripts/auth-heavy.js \
      --out json=/results/auth-$(date +%Y%m%d-%H%M).json

stress-all: stress-smoke stress-bf stress-burst
    @echo "Stress suite finalizada. Resultados em stress-test/results/"

# ---------------------------------------------------------------------------
# Chaos
# ---------------------------------------------------------------------------

chaos-kill:
    chmod +x stress-test/chaos/kill-node.sh
    ./stress-test/chaos/kill-node.sh {{node}} {{down_secs}}

chaos-loop:
    chmod +x stress-test/chaos/*.sh
    ./stress-test/chaos/chaos-loop.sh {{cycles}} {{down_secs}}

# ---------------------------------------------------------------------------
# Fluxos prontos
# ---------------------------------------------------------------------------

# Stack BF + enquete
bf-ready: up
    @echo "Aguardando health..."
    @for i in $(seq 1 30); do curl -sf "{{base_url}}/health" >/dev/null && break; sleep 2; done
    just setup-poll
    @echo ""
    @echo "Pronto. Exemplos: just stress-smoke | just stress-bf | just chaos-kill"

# Compose normal + enquete
dev-ready: dev-up
    @echo "Aguardando health..."
    @for i in $(seq 1 30); do curl -sf "{{base_url_dev}}/health" >/dev/null && break; sleep 2; done
    just dev-setup-poll
    @echo "Pronto em {{base_url_dev}}"

bf-test: bf-ready stress-smoke stress-bf
    @echo "Load ok. Caos sob carga: just chaos-kill"

# ---------------------------------------------------------------------------
# Limpeza
# ---------------------------------------------------------------------------

clean:
    rm -rf bin/ coverage/
    rm -f stress-test/results/*.json
    @echo "bin/, coverage/ e results k6 limpos."

clean-data:
    rm -rf data/*
    @echo "data/ limpo."

# Para BF + normal e limpa artefatos locais
clean-all: down dev-down clean
    @echo "Stacks parados e artefatos limpos."
