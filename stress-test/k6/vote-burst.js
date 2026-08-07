/**
 * Cenário de rajada de votos (pico de abertura da campanha).
 *
 * Pressiona fortemente POST /polls/{id}/vote e GET /results.
 * Sem cookie de autenticação a maioria retorna 401, mas o caminho
 * de rate-limit + validação + (eventual) SQLite ainda é exercitado.
 *
 * Para votos reais bem-sucedidos é necessário pré-popular votantes
 * verificados ou automatizar o fluxo de verify (veja README).
 */
import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';
const POLL_ID = __ENV.POLL_ID || '1';

const errorRate = new Rate('errors');
const voteLatency = new Trend('vote_latency', true);
const resultsLatency = new Trend('results_latency', true);
const votesOk = new Counter('votes_ok');
const votesConflict = new Counter('votes_conflict');
const rateLimited = new Counter('rate_limited');
const unauthorized = new Counter('unauthorized');

function fakeCpf(vu, iter) {
  const n = 30000000000 + vu * 20000 + (iter % 20000);
  return String(n).padStart(11, '0').slice(0, 11);
}

export const options = {
  // Spike agressivo
  stages: [
    { duration: '20s', target: 100 },
    { duration: '30s', target: 600 },  // abertura da campanha
    { duration: '1m', target: 1000 },  // stress máximo
    { duration: '30s', target: 200 },
    { duration: '20s', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(95)<3000'],
    errors: ['rate<0.15'],
  },
};

export default function () {
  const cpf = fakeCpf(__VU, __ITER);

  group('Vote Burst', () => {
    const payload = JSON.stringify({
      cpf,
      answer_ids: [((__VU + __ITER) % 5) + 1], // distribui entre as respostas
    });
    const res = http.post(`${BASE}/polls/${POLL_ID}/vote`, payload, {
      headers: { 'Content-Type': 'application/json' },
      tags: { name: 'vote' },
    });
    voteLatency.add(res.timings.duration);

    if (res.status === 201 || res.status === 200) {
      votesOk.add(1);
    } else if (res.status === 409) {
      votesConflict.add(1);
    } else if (res.status === 429) {
      rateLimited.add(1);
    } else if (res.status === 401) {
      unauthorized.add(1);
    } else {
      errorRate.add(1);
    }
  });

  // 30% das iterações também consultam resultados (dashboard / usuários atualizando)
  if (Math.random() < 0.3) {
    group('Results', () => {
      const res = http.get(`${BASE}/polls/${POLL_ID}/results`, {
        tags: { name: 'results' },
      });
      resultsLatency.add(res.timings.duration);
      check(res, {
        'results ok': (r) => r.status === 200 || r.status === 410,
      });
    });
  }

  sleep(0.05 + Math.random() * 0.25);
}
