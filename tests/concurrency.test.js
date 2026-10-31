// Real contention: many requests in flight at once over a pool of separate PostgreSQL connections.
import { afterEach, beforeEach, describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { createFixture } from './helpers.js';

describe('concurrency safety', () => {
  let f;
  beforeEach(async () => { f = await createFixture(); });
  afterEach(() => f.cleanup());

  const parallel = (n, make) => Promise.all(Array.from({ length: n }, (_, i) => f.service.createTransfer(make(i))));
  const req = (from, to, amount, idempotencyKey) => ({ fromWalletId: from, toWalletId: to, amount, idempotencyKey });

  it('prevents double spending: 30 parallel debits of 10 against a balance of 100', async () => {
    await f.wallet('src', 100); await f.wallet('dst', 0);
    const results = await parallel(30, (i) => req('src', 'dst', 10, `k${i}`));

    const statuses = results.map((r) => r.transfer.status);
    assert.equal(statuses.filter((s) => s === 'PROCESSED').length, 10);
    assert.equal(statuses.filter((s) => s === 'FAILED').length, 20);
    assert.deepEqual([await f.balance('src'), await f.balance('dst')], [0, 100]);
    assert.equal(await f.count('ledger_entries'), 20); // 10 processed transfers x 2 entries
    await f.assertIntegrity();
  });

  it('executes concurrent duplicates of one idempotent request exactly once', async () => {
    await f.wallet('src', 1000); await f.wallet('dst', 0);
    const results = await parallel(20, () => req('src', 'dst', 100, 'same'));

    assert.equal(new Set(results.map((r) => r.transfer.id)).size, 1);
    assert.equal(results.filter((r) => !r.replayed).length, 1);
    assert.deepEqual([await f.balance('src'), await f.balance('dst')], [900, 100]);
    assert.equal(await f.count('transfers'), 1);
    assert.equal(await f.count('ledger_entries'), 2);
    await f.assertIntegrity();
  });

  it('opposing transfers A<->B neither deadlock nor lose money (ordered row locks)', async () => {
    await f.wallet('a', 1000); await f.wallet('b', 1000);
    const results = await parallel(40, (i) => (i % 2 === 0 ? req('a', 'b', 10, `x${i}`) : req('b', 'a', 10, `x${i}`)));

    assert.ok(results.every((r) => r.transfer.status === 'PROCESSED'));
    assert.equal((await f.balance('a')) + (await f.balance('b')), 2000);
    assert.deepEqual([await f.balance('a'), await f.balance('b')], [1000, 1000]); // 20 each way
    await f.assertIntegrity();
  });

  it('keeps global invariants across many wallets in a ring', async () => {
    for (let i = 0; i < 5; i += 1) await f.wallet(`w${i}`, 200);
    await parallel(50, (i) => req(`w${i % 5}`, `w${(i + 1) % 5}`, 7, `m${i}`));
    let total = 0;
    for (let i = 0; i < 5; i += 1) total += await f.balance(`w${i}`);
    assert.equal(total, 1000);
    await f.assertIntegrity();
  });
});
