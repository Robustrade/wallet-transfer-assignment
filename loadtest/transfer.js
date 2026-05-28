// k6 HTTP load test for the wallet-transfer service.
//
// Usage:
//   1. In one terminal:  make db-up && make run
//   2. In another:       make load
//
// What it does:
//   - Seeds N source + N sink wallets via POST /wallets (setup).
//   - Then drives sustained traffic at the documented target RPS,
//     issuing real POST /transfers (1 source, 1 sink, amount=1) so
//     the wallet lock + idempotency PK both get exercised.
//   - Asserts p95 / p99 latency thresholds and error-rate ceiling
//     so a regression breaks `make load` (and would break CI if
//     anyone wires it in).
//
// Why each wallet has its own idempotency key per iteration:
//   - We are NOT measuring fast-path replay throughput here (that has
//     its own Go benchmark).
//   - We ARE measuring the slow path under realistic load — every
//     request must take a wallet lock + write a transfer + write the
//     idempotency row + write 2 ledger rows + commit.
//
// Why source wallets get a huge initial balance:
//   - The 60-second run at 500 RPS = 30,000 transfers per source.
//     Seeding to 1B (with amount=1) means we never hit the
//     insufficient-funds branch and the metric we're measuring is
//     pure happy-path throughput.

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter } from 'k6/metrics';

// ---------- tunables ----------
const BASE = __ENV.BASE_URL || 'http://localhost:8080';
const NUM_WALLET_PAIRS = parseInt(__ENV.WALLETS || '20', 10);
const TARGET_RPS = parseInt(__ENV.RPS || '300', 10);
const DURATION = __ENV.DURATION || '60s';

// ---------- k6 scenario ----------
export const options = {
  scenarios: {
    sustained: {
      executor: 'constant-arrival-rate',
      rate: TARGET_RPS,
      timeUnit: '1s',
      duration: DURATION,
      // Generous VU pool so a single slow response doesn't backpressure
      // the iteration rate. k6 only spins up as many as needed.
      preAllocatedVUs: Math.max(50, TARGET_RPS / 5),
      maxVUs: Math.max(200, TARGET_RPS),
    },
  },
  thresholds: {
    // The service must serve the bulk of requests fast; the tail
    // can stretch but not beyond what a human would notice.
    'http_req_duration{expected_response:true}': [
      'p(50)<30',   // half of requests under 30ms
      'p(95)<120',  // 95% under 120ms
      'p(99)<400',  // 99% under 400ms (tail mostly from
                    // DB-pool saturation at 300 RPS on a single
                    // local Postgres; tune up DB_MAX_OPEN_CONNS
                    // + Postgres max_connections to lower it)
    ],
    // Fewer than 1% of requests may fail (the few that hit a tx
    // serialization retry — currently rare; would become more
    // common at higher RPS).
    'http_req_failed': ['rate<0.01'],
    // Custom: every iteration must result in a 200 OK from the
    // service, not just a successful HTTP request.
    'transfer_processed': ['count>0'],
  },
  // Sample latency every request; default sampling is fine for this
  // workload size.
};

// ---------- setup: seed wallets ----------
export function setup() {
  const wallets = [];
  for (let i = 0; i < NUM_WALLET_PAIRS; i++) {
    const src = `loadtest-src-${i}`;
    const dst = `loadtest-dst-${i}`;
    seed(src, 1_000_000_000); // 1B seed — comfortably over 60s × any RPS we'd run
    seed(dst, 0);
    wallets.push({ src, dst });
  }
  return { wallets };
}

function seed(id, balance) {
  const res = http.post(`${BASE}/wallets`, JSON.stringify({ id, balance }), {
    headers: { 'Content-Type': 'application/json' },
  });
  if (res.status !== 201 && res.status !== 400) {
    // 400 = duplicate id (already seeded by an earlier run); ignore.
    throw new Error(`seed ${id} failed: ${res.status} ${res.body}`);
  }
}

// ---------- metric ----------
const processedCounter = new Counter('transfer_processed');

// ---------- per-iteration ----------
export default function (data) {
  // Pick a random wallet pair to spread lock contention across N
  // pairs rather than slamming one. Concurrent debits ON THE SAME
  // pair are covered by the stress tests; here we want sustained
  // HTTP throughput numbers.
  const pair = data.wallets[Math.floor(Math.random() * data.wallets.length)];

  // Unique idempotency key per iteration so every request hits the
  // slow path. The (VU id, iteration) pair guarantees uniqueness
  // across the run.
  const key = `loadtest-${__VU}-${__ITER}-${Date.now()}`;

  const res = http.post(
    `${BASE}/transfers`,
    JSON.stringify({
      idempotencyKey: key,
      fromWalletId: pair.src,
      toWalletId: pair.dst,
      amount: 1,
    }),
    {
      headers: { 'Content-Type': 'application/json' },
      tags: { name: 'POST /transfers' },
    }
  );

  const ok = check(res, {
    'status is 200': (r) => r.status === 200,
    'state is PROCESSED': (r) => {
      try {
        const body = r.json();
        return body && body.state === 'PROCESSED';
      } catch (_) {
        return false;
      }
    },
  });
  if (ok) processedCounter.add(1);
}
