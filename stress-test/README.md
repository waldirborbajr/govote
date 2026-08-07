# Stress + Chaos — Black Friday (govote)

Testes de carga e resiliência contra o stack `docker-compose.bf.yml`
(Nginx + Redis + 3× API + vote-worker).

## Pré-requisitos

```bash
# Na raiz do projeto
cp .env.bf.example .env
docker compose -f docker-compose.bf.yml up -d --build
docker compose -f docker-compose.bf.yml ps
```

Entrada pública: **http://localhost:8080**

## Setup da enquete

```bash
chmod +x scripts/*.sh chaos/*.sh
./scripts/setup-poll.sh http://localhost:8080
export POLL_ID=1   # ajuste
```

## Ordem de testes

### 1. Smoke

```bash
docker compose -f docker-compose.stress.yml --profile stress run --rm \
  -e BASE_URL=http://localhost:8080 -e POLL_ID=$POLL_ID \
  k6 run /scripts/smoke.js
```

### 2. Load / Black Friday

```bash
docker compose -f docker-compose.stress.yml --profile stress run --rm \
  -e BASE_URL=http://localhost:8080 -e POLL_ID=$POLL_ID \
  k6 run /scripts/blackfriday.js --out json=/results/bf-$(date +%Y%m%d-%H%M).json
```

### 3. Vote burst

```bash
docker compose -f docker-compose.stress.yml --profile stress run --rm \
  -e BASE_URL=http://localhost:8080 -e POLL_ID=$POLL_ID \
  k6 run /scripts/vote-burst.js --out json=/results/burst-$(date +%Y%m%d-%H%M).json
```

### 4. Chaos (queda e volta de nó sob carga)

Terminal A — stress contínuo (ex.: blackfriday ou vote-burst).

Terminal B:

```bash
# Mata govote-2 por 90s e sobe de novo
./chaos/kill-node.sh govote-2 90

# Ou loop de 3 ciclos
./chaos/chaos-loop.sh 3 60
```

**O que observar**

- Nginx para de enviar tráfego ao nó morto (`max_fails` / `fail_timeout`)
- p95 sobe de forma controlada; 502/504 devem ser baixos (retry upstream)
- Redis e vote-worker seguem processando
- Ao voltar, o nó entra no pool sem thundering herd destrutivo

```bash
docker stats govote-1 govote-2 govote-3 govote-nginx govote-redis govote-vote-worker
docker logs -f govote-nginx
docker logs -f govote-vote-worker
```

## Critérios de aceite sugeridos

| Métrica | Alvo |
|---------|------|
| Erros 5xx durante kill de 1 nó | < 2% das requests |
| Recuperação health do nó | < 30s após `docker start` |
| p95 leitura (polls/results) com cache | < 300 ms |
| Fila Redis não explode sem drenar | worker consome continuamente |

## Limpeza

```bash
docker compose -f ../docker-compose.bf.yml down
# dados locais:
# rm -rf ../data/*
```
