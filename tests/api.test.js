import { after, before, beforeEach, afterEach, describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { createApp } from '../src/handler/index.js';
import { createFixture } from './helpers.js';

describe('HTTP API', () => {
  let f; let server; let base;

  beforeEach(async () => {
    f = await createFixture();
    server = createApp(f.service, { logger: { error() {} } });
    await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
    base = `http://127.0.0.1:${server.address().port}`;
    await post('/wallets', { id: 'wallet_1', initialBalance: 500 });
    await post('/wallets', { id: 'wallet_2', initialBalance: 0 });
  });
  afterEach(async () => {
    await new Promise((resolve) => server.close(resolve));
    await f.cleanup();
  });

  const call = async (method, path, body) => {
    const res = await fetch(base + path, {
      method, headers: { 'content-type': 'application/json' },
      body: body === undefined ? undefined : (typeof body === 'string' ? body : JSON.stringify(body)),
    });
    return { status: res.status, headers: res.headers, body: await res.json() };
  };
  const post = (path, body) => call('POST', path, body);
  const get = (path) => call('GET', path);
  const transfer = (over = {}) => post('/transfers', {
    idempotencyKey: 'abc123', fromWalletId: 'wallet_1', toWalletId: 'wallet_2', amount: 100, ...over,
  });

  it('POST /transfers -> 201 with the contract fields', async () => {
    const r = await transfer();
    assert.equal(r.status, 201);
    assert.deepEqual(
      Object.fromEntries(['idempotencyKey', 'fromWalletId', 'toWalletId', 'amount', 'status', 'failureReason'].map((k) => [k, r.body[k]])),
      { idempotencyKey: 'abc123', fromWalletId: 'wallet_1', toWalletId: 'wallet_2', amount: 100, status: 'PROCESSED', failureReason: null },
    );
    assert.equal(r.headers.get('idempotent-replayed'), null);
  });

  it('duplicate request -> 200, replay header, identical body, balances unchanged', async () => {
    const first = await transfer();
    const r = await transfer();
    assert.equal(r.status, 200);
    assert.equal(r.headers.get('idempotent-replayed'), 'true');
    assert.deepEqual(r.body, first.body);
    assert.equal((await get('/wallets/wallet_1')).body.balance, 400);
  });

  it('FAILED transfer is 422 for the first call and for replays', async () => {
    for (let i = 0; i < 2; i += 1) {
      const r = await transfer({ amount: 10_000 });
      assert.equal(r.status, 422);
      assert.deepEqual([r.body.status, r.body.failureReason], ['FAILED', 'INSUFFICIENT_FUNDS']);
    }
  });

  it('same key, different payload -> 409', async () => {
    await transfer();
    const r = await transfer({ amount: 1 });
    assert.deepEqual([r.status, r.body.error.code], [409, 'IDEMPOTENCY_KEY_CONFLICT']);
  });

  it('validation errors -> 400', async () => {
    for (const over of [{ amount: 0 }, { amount: '5' }, { amount: 1.5 }, { amount: true }, { toWalletId: 'wallet_1' }, { fromWalletId: null }, { idempotencyKey: 42 }]) {
      const r = await transfer(over);
      assert.deepEqual([r.status, r.body.error.code], [400, 'VALIDATION_ERROR'], JSON.stringify(over));
    }
  });

  it('malformed or non-object bodies -> 400', async () => {
    assert.equal((await post('/transfers', 'not json')).status, 400);
    assert.equal((await post('/transfers', '[1,2]')).status, 400);
    assert.equal((await post('/transfers', 'null')).status, 400);
    assert.equal((await call('POST', '/transfers')).status, 400);
  });

  it('unknown wallet -> 404', async () => {
    const r = await transfer({ toWalletId: 'nope' });
    assert.deepEqual([r.status, r.body.error.code], [404, 'WALLET_NOT_FOUND']);
  });

  it('GET transfer, balance and ledger', async () => {
    const t = (await transfer()).body;
    assert.deepEqual((await get(`/transfers/${t.id}`)).body, t);
    assert.equal((await get('/transfers/missing')).status, 404);
    assert.equal((await get('/wallets/wallet_2')).body.balance, 100);
    const ledger = (await get('/wallets/wallet_1/ledger')).body;
    assert.deepEqual(ledger.map((e) => [e.type, e.amount, e.transferId]), [['DEBIT', 100, t.id]]);
    assert.equal((await get('/wallets/ghost')).status, 404);
  });

  it('wallet creation rules', async () => {
    assert.equal((await post('/wallets', { id: 'wallet_1' })).status, 409);
    assert.equal((await post('/wallets', { id: 'x', initialBalance: -1 })).status, 400);
    assert.equal((await post('/wallets', { initialBalance: 5 })).status, 400);
  });

  it('unknown route -> 404, wrong method -> 405', async () => {
    assert.equal((await get('/nope')).status, 404);
    assert.equal((await get('/transfers')).status, 405);
  });
});
