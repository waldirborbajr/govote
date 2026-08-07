# Black Friday — Arquitetura de escala (Nível B)

## O que foi implementado

| Componente | Função |
|------------|--------|
| **Redis** | Rate-limit compartilhado, cache de polls/results, set “já votou”, fila de votos (Stream) |
| **Nginx** | Load balancer `least_conn`, health via `/health`, `max_fails`/`fail_timeout`, retry upstream |
| **govote-1/2/3** | Réplicas HTTP stateless (leituras + enqueue) |
| **vote-worker** | Único consumidor da fila → grava SQLite (serializa writes de voto) |
| **SQLite WAL** | `journal_mode=WAL`, `busy_timeout=5000`, `synchronous=NORMAL` |
| **Stress + chaos** | k6 + scripts de kill/restart de nó |

## Variáveis novas

| Env | Default | Descrição |
|-----|---------|-----------|
| `GOVOTE_REDIS_URL` | vazio | Ex.: `redis://redis:6379/0` — ativa Redis |
| `GOVOTE_VOTE_ASYNC` | false | `true` = votos vão para a fila Redis |
| `GOVOTE_VOTE_WORKER` | false | `true` = processo só consome a fila (sem HTTP) |

Sem Redis o app continua no modo original (rate-limit local, votes síncronos).

## Subir o stack

```bash
cp .env.bf.example .env
docker compose -f docker-compose.bf.yml up -d --build
# API pública: http://localhost:8080
```

## Fluxo de voto (async)

1. Cliente autenticado → `POST /polls/{id}/vote`
2. API verifica token + cache `voted:{poll}:{hash}`
3. Enfileira no Redis Stream `govote:votes`
4. Responde **202 Accepted** `{ "voted": true, "async": true }`
5. `vote-worker` consome, chama `CastVote`, marca votado, invalida cache de results

## Limitações conscientes (evento 24h)

- Auth (passcode/verify) ainda escreve no SQLite a partir de qualquer réplica API. Em um único host Docker com volume compartilhado + WAL costuma funcionar; em multi-host use NFS cuidadoso ou centralize writes de auth.
- SQLite permanece a fonte da verdade; não é substituto de Postgres para multi-região.
- Cache de results com TTL curto (3s) — “quase tempo real”.

## Stress e caos

Ver `stress-test/README.md`.

## Plano B

Se o stress mostrar contenção residual alta em writes de auth ou fila sem drenar: migrar tabela `votes` (e opcionalmente `voters`) para Postgres, mantendo o mesmo contrato de API.
