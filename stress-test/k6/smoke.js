/**
 * Smoke test rápido — valida que o ambiente está saudável antes do stress.
 * Rode sempre primeiro.
 */
import http from 'k6/http';
import { check, sleep } from 'k6';

const BASE = __ENV.BASE_URL || 'http://localhost:9080';
const POLL_ID = __ENV.POLL_ID || '1';

export const options = {
  vus: 3,
  duration: '30s',
  thresholds: {
    http_req_failed: ['rate<0.05'],
    http_req_duration: ['p(95)<1500'],
  },
};

export default function () {
  // Health
  let res = http.get(`${BASE}/health`);
  check(res, { 'health 200': (r) => r.status === 200 });

  // List polls
  res = http.get(`${BASE}/polls`);
  check(res, { 'polls 200': (r) => r.status === 200 });

  // Results (pode ser 200 ou 410 se a enquete ainda não existir)
  res = http.get(`${BASE}/polls/${POLL_ID}/results`);
  check(res, {
    'results reachable': (r) => r.status === 200 || r.status === 410 || r.status === 404,
  });

  // Request passcode (gera carga mínima de escrita)
  const cpf = `9${String(1000000000 + __VU * 100 + __ITER).padStart(10, '0')}`.slice(0, 11);
  res = http.post(
    `${BASE}/auth/request-passcode`,
    JSON.stringify({
      cpf,
      name: `Smoke ${cpf}`,
      phone: `+5511987654321`,
    }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  check(res, {
    'passcode accepted or rate-limited': (r) =>
      r.status === 200 || r.status === 429 || r.status === 400,
  });

  sleep(1);
}
