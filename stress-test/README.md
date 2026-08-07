# Stress Test Plan — govote (Black Friday)

Plano de testes de stress em Docker para identificar pontos frágeis antes da campanha.

## Pontos frágeis conhecidos do sistema

| Componente              | Risco                                      | Sintoma típico                  |
|-------------------------|--------------------------------------------|---------------------------------|
| SQLite (`MaxOpenConns=1`)| Único writer — contenção sob writes        | `SQLITE_BUSY`, timeouts em /vote |
| Rate-limit por IP       | Bloqueio precoce (default 10/60s)          | HTTP 429                        |
| `mem_limit` / GOMEMLIMIT| OOM em picos                               | Container reinicia              |
| Mapa do rate-limiter    | Crescimento de memória em processos longos | Aumento contínuo de RAM         |
| Lockout por CPF         | Bloqueio de usuários legítimos             | 401/403 após falhas             |

## Pré-requisitos

- Docker + Docker Compose v2
- Portas 9080 e 8443 livres
- ~512 MB de RAM disponível para o container govote

## Subir o ambiente

```bash
# 1. Entrar na pasta
cd stress-test

# 2. (Opcional) ajustar secrets
cp .env.stress .env

# 3. Subir o govote
docker compose -f docker-compose.stress.yml up -d govote

# 4. Aguardar healthy
docker compose -f docker-compose.stress.yml ps
docker logs -f govote-stress   # Ctrl+C quando aparecer o health
```

## Criar a enquete de teste

```bash
chmod +x scripts/setup-poll.sh
./scripts/setup-poll.sh http://localhost:9080

# Anote o POLL_ID retornado e exporte:
export POLL_ID=1   # ajuste conforme a resposta
```

## Ordem recomendada de execução

### 1. Smoke (obrigatório)

```bash
docker compose -f docker-compose.stress.yml --profile stress run --rm \
  -e BASE_URL=http://govote:9080 \
  -e POLL_ID=${POLL_ID:-1} \
  k6 run /scripts/smoke.js
```

### 2. Auth heavy (pressão em request-passcode)

```bash
docker compose -f docker-compose.stress.yml --profile stress run --rm \
  -e BASE_URL=http://govote:9080 \
  k6 run /scripts/auth-heavy.js \
  --out json=/results/auth-heavy-$(date +%Y%m%d-%H%M).json
```

### 3. Black Friday (fluxo completo)

```bash
docker compose -f docker-compose.stress.yml --profile stress run --rm \
  -e BASE_URL=http://govote:9080 \
  -e POLL_ID=${POLL_ID:-1} \
  k6 run /scripts/blackfriday.js \
  --out json=/results/blackfriday-$(date +%Y%m%d-%H%M).json
```

### 4. Vote burst (spike de abertura)

```bash
docker compose -f docker-compose.stress.yml --profile stress run --rm \
  -e BASE_URL=http://govote:9080 \
  -e POLL_ID=${POLL_ID:-1} \
  k6 run /scripts/vote-burst.js \
  --out json=/results/vote-burst-$(date +%Y%m%d-%H%M).json
```

## Monitoramento durante o teste

Em outro terminal:

```bash
# Recursos
docker stats govote-stress

# Logs (procure por SQLITE_BUSY, OOM, panic)
docker logs -f govote-stress

# Tamanho do banco
docker exec govote-stress ls -lh /data/votes.db 2>/dev/null || true
```

## Interpretação rápida dos resultados k6

- `http_req_duration{p(95)}` > 2–3 s → latência degradada
- `errors` rate > 5–8 % → problema real (desconte 401/429 esperados)
- `rate_limited` alto → rate-limit ainda restritivo (já está em 200/min)
- `votes_ok` baixo + muitos `unauthorized` → esperado sem cookie `voter_token`
- Container reinicia ou `OOMKilled` → aumentar `mem_limit` / `GOMEMLIMIT`

## Limpeza

```bash
docker compose -f docker-compose.stress.yml down
# Remove também o volume de dados de teste (cuidado):
# docker compose -f docker-compose.stress.yml down -v
# rm -rf data/*
```

## Próximos passos após os testes

1. Registrar o ponto de quebra (VUs / RPS em que p95 estoura ou erros sobem).
2. Se `SQLITE_BUSY` aparecer cedo → considerar Postgres ou fila de votos.
3. Ajustar rate-limit e lockout para o volume esperado da campanha.
4. Repetir o soak test (carga moderada por 30–60 min) para detectar vazamentos.
