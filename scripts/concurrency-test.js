const crypto = require('node:crypto');

const API_URL = process.env.API_URL || 'http://localhost:8090/transfers';
const MODE = process.env.MODE || 'opposite';
const COUNT = Number(process.env.COUNT || 100);
const WALLET_A = Number(process.env.WALLET_A || 1);
const WALLET_B = Number(process.env.WALLET_B || 2);
const MIN_WALLET_ID = Number(process.env.MIN_WALLET_ID || 1);
const MAX_WALLET_ID = Number(process.env.MAX_WALLET_ID || 100);
const AMOUNT = Number(process.env.AMOUNT || 100);
const TIMEOUT_MS = Number(process.env.TIMEOUT_MS || 30000);

const randomInt = (min, max) => crypto.randomInt(min, max + 1);
const sharedKey = crypto.randomUUID();
const nowIST = () => new Date().toLocaleTimeString('en-IN', {
  timeZone: 'Asia/Kolkata', hour12: false, fractionalSecondDigits: 3,
});

function makePayload(index) {
  let fromWalletId, toWalletId;

  if (MODE === 'random') {
    fromWalletId = randomInt(MIN_WALLET_ID, MAX_WALLET_ID);
    do {
      toWalletId = randomInt(MIN_WALLET_ID, MAX_WALLET_ID);
    } while (toWalletId === fromWalletId);
  } else if (MODE === 'opposite') {
    const forward = index % 2 === 0;
    fromWalletId = forward ? WALLET_A : WALLET_B;
    toWalletId = forward ? WALLET_B : WALLET_A;
  } else {
    fromWalletId = WALLET_A;
    toWalletId = WALLET_B;
  }

  return {
    idempotencyKey: MODE === 'same-key' ? sharedKey : crypto.randomUUID(),
    fromWalletId,
    toWalletId,
    amount: MODE === 'random' ? randomInt(100, 10000) : AMOUNT,
  };
}

async function send(index) {
  const payload = makePayload(index);
  const requestTime = nowIST();
  const started = performance.now();

  try {
    const response = await fetch(API_URL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    const body = await response.text();
    return {
      request: index + 1,
      from: payload.fromWalletId,
      to: payload.toWalletId,
      amount: payload.amount,
      requestTime,
      responseTime: nowIST(),
      durationMs: +(performance.now() - started).toFixed(2),
      status: response.status,
      body,
    };
  } catch (error) {
    return {
      request: index + 1,
      from: payload.fromWalletId,
      to: payload.toWalletId,
      amount: payload.amount,
      requestTime,
      responseTime: nowIST(),
      durationMs: +(performance.now() - started).toFixed(2),
      status: 'NETWORK_ERROR',
      body: error.message,
    };
  }
}

function percentile(sorted, p) {
  if (!sorted.length) return null;
  return sorted[Math.ceil(p / 100 * sorted.length) - 1];
}

async function main() {
  if (!Number.isInteger(COUNT) || COUNT < 1) throw new Error('Invalid COUNT');
  if (WALLET_A === WALLET_B && MODE !== 'random') throw new Error('A and B must differ');
  if (MODE === 'random' && MIN_WALLET_ID >= MAX_WALLET_ID) {
    throw new Error('Random wallet range must contain at least two IDs');
  }

  console.log({ API_URL, MODE, COUNT, TIMEOUT_MS });
  const start = performance.now();
  const results = await Promise.all(
    Array.from({ length: COUNT }, (_, i) => send(i))
  );
  console.table(results);

  const durations = results.map(r => r.durationMs).sort((a, b) => a - b);
  const countStatus = status => results.filter(r => r.status === status).length;
  const otherHTTP = results.filter(r =>
    typeof r.status === 'number' && ![201, 409].includes(r.status)
  ).length;

  console.table({
    requests: COUNT,
    processed201: countStatus(201),
    rejected409: countStatus(409),
    otherHTTP,
    networkErrors: countStatus('NETWORK_ERROR'),
    minMs: durations[0],
    p50Ms: percentile(durations, 50),
    p95Ms: percentile(durations, 95),
    p99Ms: percentile(durations, 99),
    maxMs: durations.at(-1),
    elapsedMs: +(performance.now() - start).toFixed(2),
  });

  if (MODE === 'same-key') {
    const distinctResponses = new Set(results.map(r => `${r.status}:${r.body}`));
    console.log('Same-key distinct HTTP responses:', distinctResponses.size);
    console.log('Idempotency key:', sharedKey);
  }
}

main().catch(err => { console.error(err); process.exitCode = 1; });
