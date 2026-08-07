/**
 * Cenário focado em autenticação (request-passcode + verify)
 * Simula fila grande de usuários pedindo código (WhatsApp/Telegram).
 */
import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'http://localhost:9080';

const errorRate = new Rate('errors');
const authLatency = new Trend('auth_latency', true);
const rateLimited = new Counter('rate_limited');

function fakeCpf(vu, iter) {
  const n = 20000000000 + vu * 50000 + (iter % 50000);
  return String(n).padStart(11, '0').slice(0, 11);
}

export const options = {
  stages: [
    { duration: '30s', target: 30 },
    { duration: '2m', target: 150 },
    { duration: '2m', target: 400 },
    { duration: '1m', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(95)<2000'],
    errors: ['rate<0.10'],
  },
};

export default function () {
  const cpf = fakeCpf(__VU, __ITER);
  const name = `Auth User ${cpf}`;
  const phone = `+55119${String(20000000 + (__VU % 80000000)).padStart(8, '0')}`;

  group('Request Passcode', () => {
    const res = http.post(
      `${BASE}/auth/request-passcode`,
      JSON.stringify({ cpf, name, phone }),
      { headers: { 'Content-Type': 'application/json' } }
    );
    authLatency.add(res.timings.duration);

    const ok = check(res, {
      'status 200': (r) => r.status === 200,
    });
    if (res.status === 429) {
      rateLimited.add(1);
    } else {
      errorRate.add(!ok);
    }
  });

  // O passcode real só aparece nos logs do servidor (PoC) ou via Telegram.
  // Aqui apenas pressionamos o endpoint de request para medir contenção SQLite.

  sleep(0.15 + Math.random() * 0.4);
}
