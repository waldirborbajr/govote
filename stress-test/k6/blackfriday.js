/**
 * Cenário principal — Black Friday (fluxo completo)
 *
 * Stages:
 *   warm-up → carga normal → pico → stress máximo → ramp-down
 *
 * Variáveis de ambiente:
 *   BASE_URL  (default: http://localhost:8080)
 *   POLL_ID   (default: 1)
 */
import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';
const POLL_ID = __ENV.POLL_ID || '1';

const errorRate = new Rate('errors');
const voteLatency = new Trend('vote_latency', true);
const authLatency = new Trend('auth_latency', true);
const listLatency = new Trend('list_latency', true);
const votesOk = new Counter('votes_ok');
const votesConflict = new Counter('votes_conflict');
const rateLimited = new Counter('rate_limited');

function fakeCpf(vu, iter) {
  // Gera CPF numérico único por VU + iteração (11 dígitos)
  const n = 10000000000 + vu * 100000 + (iter % 100000);
  return String(n).padStart(11, '0').slice(0, 11);
}

export const options = {
  stages: [
    { duration: '1m', target: 50 },   // warm-up
    { duration: '3m', target: 200 },  // carga normal de campanha
    { duration: '2m', target: 500 },  // pico Black Friday
    { duration: '2m', target: 800 },  // stress máximo
    { duration: '1m', target: 0 },    // ramp-down
  ],
  thresholds: {
    http_req_duration: ['p(95)<2500'],
    errors: ['rate<0.08'],
    vote_latency: ['p(95)<2000'],
  },
  // Descomente para limitar VUs máximos se a máquina for fraca
  // vus: 100,
  // duration: '5m',
};

export default function () {
  const cpf = fakeCpf(__VU, __ITER);
  const name = `BF User ${cpf}`;
  const phone = `+55119${String(10000000 + (__VU % 90000000)).padStart(8, '0')}`;

  group('1. Request Passcode', () => {
    const res = http.post(
      `${BASE}/auth/request-passcode`,
      JSON.stringify({ cpf, name, phone }),
      { headers: { 'Content-Type': 'application/json' }, tags: { name: 'request_passcode' } }
    );
    authLatency.add(res.timings.duration);
    const ok = check(res, {
      'passcode status 200': (r) => r.status === 200,
    });
    if (res.status === 429) rateLimited.add(1);
    errorRate.add(!ok && res.status !== 429);
  });

  // Em ambiente real o passcode chega via WhatsApp/Telegram.
  // Para stress de voto puro, o cenário vote-burst.js é mais adequado.
  // Aqui mantemos o request-passcode para pressionar auth + SQLite.

  group('2. List Polls', () => {
    const res = http.get(`${BASE}/polls`, { tags: { name: 'list_polls' } });
    listLatency.add(res.timings.duration);
    check(res, { 'polls 200': (r) => r.status === 200 });
    if (res.status >= 400) errorRate.add(1);
  });

  group('3. Vote', () => {
    // Nota: o endpoint exige autenticação (cookie voter_token após /auth/verify).
    // Sem o cookie o servidor retorna 401. Isso ainda gera carga útil no path
    // de validação + rate-limit. Para votos reais use o fluxo com verify
    // ou o script vote-burst com pré-população.
    const payload = JSON.stringify({
      cpf,
      answer_ids: [1],
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
      // Esperado sem cookie — não conta como erro de sistema
    } else {
      errorRate.add(1);
    }
  });

  group('4. Results', () => {
    const res = http.get(`${BASE}/polls/${POLL_ID}/results`, {
      tags: { name: 'results' },
    });
    check(res, { 'results 200 or 410': (r) => r.status === 200 || r.status === 410 });
  });

  sleep(0.2 + Math.random() * 0.6);
}
