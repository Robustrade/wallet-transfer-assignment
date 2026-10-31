import { afterEach, beforeEach, describe, it, mock } from 'node:test';
import assert from 'node:assert/strict';
import {
  EntryType, IdempotencyKeyConflict, TransferNotFound, TransferStatus, ValidationError, WalletNotFound,
} from '../src/domain/index.js';
import { LedgerRepository, checkIntegrity } from '../src/repository/index.js';
import { createFixture } from './helpers.js';

describe('transfer execution', () => {
  let f;
  beforeEach(async () => { f = await createFixture(); await f.wallet('w1', 500); await f.wallet('w2', 50); });
  afterEach(() => f.cleanup());

  const send = (over = {}) => f.service.createTransfer({ fromWalletId: 'w1', toWalletId: 'w2', amount: 100, ...over });

  it('moves money and writes exactly two ledger entries', async () => {
    const result = await send({ idempotencyKey: 'k1' });
    assert.equal(result.replayed, false);
    assert.equal(result.transfer.status, TransferStatus.PROCESSED);
    assert.deepEqual([await f.balance('w1'), await f.balance('w2')], [400, 150]);

    const debit = (await f.service.getLedger('w1')).find((e) => e.type === EntryType.DEBIT);
    const credit = (await f.service.getLedger('w2')).find((e) => e.type === EntryType.CREDIT);
    assert.deepEqual([debit.walletId, debit.amount, debit.transferId], ['w1', 100, result.transfer.id]);
    assert.deepEqual([credit.walletId, credit.amount, credit.transferId], ['w2', 100, result.transfer.id]);
    assert.equal(await f.count('ledger_entries'), 2);
    await f.assertIntegrity();
  });

  it('can be fetched by id; unknown or malformed ids are TransferNotFound', async () => {
    const { transfer } = await send();
    assert.deepEqual(await f.service.getTransfer(transfer.id), transfer);
    await assert.rejects(f.service.getTransfer('00000000-0000-4000-8000-000000000000'), TransferNotFound);
    await assert.rejects(f.service.getTransfer('not-a-uuid'), TransferNotFound);
  });

  it('allows transferring the entire balance', async () => {
    assert.equal((await send({ amount: 500 })).transfer.status, 'PROCESSED');
    assert.equal(await f.balance('w1'), 0);
  });

  it('records insufficient funds as FAILED without side effects', async () => {
    const { transfer } = await send({ amount: 501 });
    assert.deepEqual([transfer.status, transfer.failureReason], ['FAILED', 'INSUFFICIENT_FUNDS']);
    assert.deepEqual([await f.balance('w1'), await f.balance('w2')], [500, 50]);
    assert.equal(await f.count('ledger_entries'), 0);
    assert.equal(await f.count('transfers'), 1);
    await f.assertIntegrity();
  });

  it('persists nothing for an unknown wallet', async () => {
    for (const [from, to] of [['ghost', 'w2'], ['w1', 'ghost']]) {
      await assert.rejects(send({ fromWalletId: from, toWalletId: to, idempotencyKey: 'k-ghost' }), WalletNotFound);
    }
    assert.equal(await f.count('transfers'), 0);
    assert.equal(await f.count('idempotency_records'), 0);
  });

  it('persists nothing for an invalid request', async () => {
    await assert.rejects(send({ toWalletId: 'w1' }), ValidationError);
    await assert.rejects(send({ amount: -1 }), ValidationError);
    assert.equal(await f.count('transfers'), 0);
  });

  it('does not deduplicate transfers without a key', async () => {
    await send({ amount: 10 }); await send({ amount: 10 });
    assert.equal(await f.balance('w1'), 480);
    assert.equal(await f.count('transfers'), 2);
  });

  it('rolls everything back if a failure happens after the debit and credit', async () => {
    const spy = mock.method(LedgerRepository.prototype, 'addPair', async () => { throw new Error('boom'); });
    await assert.rejects(send({ amount: 10, idempotencyKey: 'k-boom' }), /boom/);
    spy.mock.restore();

    assert.deepEqual([await f.balance('w1'), await f.balance('w2')], [500, 50]);
    for (const table of ['transfers', 'idempotency_records', 'ledger_entries']) assert.equal(await f.count(table), 0);

    // a client retry with the same key then succeeds normally (exactly once)
    assert.equal((await send({ amount: 10, idempotencyKey: 'k-boom' })).transfer.status, 'PROCESSED');
    assert.equal(await f.balance('w1'), 490);
    await f.assertIntegrity();
  });
});

describe('idempotency', () => {
  let f;
  beforeEach(async () => { f = await createFixture(); await f.wallet('w1', 500); await f.wallet('w2', 0); });
  afterEach(() => f.cleanup());

  const send = (key = 'k1', amount = 100) =>
    f.service.createTransfer({ fromWalletId: 'w1', toWalletId: 'w2', amount, idempotencyKey: key });

  it('replays return the original result and have no side effects', async () => {
    const first = await send(); const second = await send(); const third = await send();
    assert.equal(first.replayed, false);
    assert.ok(second.replayed && third.replayed);
    assert.deepEqual(second.transfer, first.transfer);
    assert.deepEqual(third.transfer, first.transfer);
    assert.deepEqual([await f.balance('w1'), await f.balance('w2')], [400, 100]);
    assert.equal(await f.count('transfers'), 1);
    assert.equal(await f.count('ledger_entries'), 2);
    await f.assertIntegrity();
  });

  it('returns the original even if state changed since (would fail if re-executed)', async () => {
    const first = await send('k1', 500);
    const replay = await send('k1', 500);
    assert.deepEqual(replay.transfer, first.transfer);
    assert.equal(replay.transfer.status, 'PROCESSED');
  });

  it('also replays FAILED results instead of re-evaluating', async () => {
    const first = await send('kf', 900);
    await f.wallet('rich', 10_000);
    await f.service.createTransfer({ fromWalletId: 'rich', toWalletId: 'w1', amount: 1000 }); // w1 can now afford it
    const replay = await send('kf', 900);
    assert.equal(first.transfer.status, 'FAILED');
    assert.ok(replay.replayed);
    assert.deepEqual(replay.transfer, first.transfer);
    await f.assertIntegrity();
  });

  it('rejects the same key with a different payload', async () => {
    await send('k1', 100);
    await assert.rejects(send('k1', 200), IdempotencyKeyConflict);
    await assert.rejects(
      f.service.createTransfer({ fromWalletId: 'w2', toWalletId: 'w1', amount: 100, idempotencyKey: 'k1' }),
      IdempotencyKeyConflict,
    );
    assert.equal(await f.balance('w1'), 400);
  });

  it('treats different keys independently', async () => {
    await send('a'); await send('b');
    assert.equal(await f.balance('w1'), 300);
  });

  it('the database itself rejects duplicate keys even if app logic is bypassed', async () => {
    const { transfer } = await send();
    await assert.rejects(
      f.db.transaction((c) => c.query("INSERT INTO idempotency_records VALUES ('k1','x',$1, now())", [transfer.id])),
      /unique/i,
    );
  });
});

describe('database guarantees (schema protects invariants even if app code is wrong)', () => {
  let f; let transfer;
  beforeEach(async () => {
    f = await createFixture(); await f.wallet('w1', 100); await f.wallet('w2', 0);
    ({ transfer } = await f.service.createTransfer({ fromWalletId: 'w1', toWalletId: 'w2', amount: 10 }));
  });
  afterEach(() => f.cleanup());

  const run = (sql, params = []) => f.db.transaction((c) => c.query(sql, params));

  it('balance cannot go negative', async () => {
    await assert.rejects(run("UPDATE wallets SET balance = -1 WHERE id = 'w1'"), /check constraint/i);
  });

  it('terminal transfer status cannot change', async () => {
    await assert.rejects(run('UPDATE transfers SET status = $1 WHERE id = $2', ['PENDING', transfer.id]), /illegal transfer state transition/);
    await assert.rejects(run("UPDATE transfers SET status = 'FAILED', failure_reason = 'x' WHERE id = $1", [transfer.id]), /illegal transfer state transition/);
  });

  it('ledger is append-only', async () => {
    await assert.rejects(run('UPDATE ledger_entries SET amount = 999'), /immutable/);
    await assert.rejects(run('DELETE FROM ledger_entries'), /immutable/);
  });

  it('allows at most one DEBIT and one CREDIT per transfer', async () => {
    await assert.rejects(run(
      "INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount, created_at) VALUES ('w1', $1, 'DEBIT', 10, now())",
      [transfer.id],
    ), /unique/i);
  });

  it('integrity check detects corruption', async () => {
    await f.db.transaction(async (c) => {
      await c.query("UPDATE wallets SET balance = balance + 1 WHERE id = 'w2'");
      assert.ok((await checkIntegrity(c)).length > 0);
    });
  });
});
