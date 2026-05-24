import { Pool } from 'pg';
import { TransferService } from '../src/service/transfer.service';
import { WalletRepository } from '../src/repository/wallet.repository';
import { TransferRepository } from '../src/repository/transfer.repository';
import { LedgerRepository } from '../src/repository/ledger.repository';
import { createTestPool, setupTestDb, teardownTestDb, seedWallets } from './helpers/db';

let pool: Pool;
let service: TransferService;

beforeAll(async () => {
  pool = createTestPool();
  await setupTestDb(pool);
  service = new TransferService(
    pool,
    new WalletRepository(pool),
    new TransferRepository(pool),
    new LedgerRepository(pool),
  );
});

afterAll(() => teardownTestDb(pool));

beforeEach(async () => {
  await pool.query('DELETE FROM ledger_entries');
  await pool.query('DELETE FROM transfers');
  await pool.query('DELETE FROM wallets');
});

describe('concurrency', () => {
  it('produces a correct final balance when multiple transfers debit the same wallet simultaneously', async () => {
    await seedWallets(pool, [
      { id: 'src', balance: 1000 },
      { id: 'dst1', balance: 0 },
      { id: 'dst2', balance: 0 },
      { id: 'dst3', balance: 0 },
      { id: 'dst4', balance: 0 },
      { id: 'dst5', balance: 0 },
    ]);

    const transfers = Array.from({ length: 5 }, (_, i) =>
      service.createTransfer({
        idempotencyKey: `conc-${i}`,
        fromWalletId: 'src',
        toWalletId: `dst${i + 1}`,
        amount: 100,
      }),
    );

    const results = await Promise.allSettled(transfers);
    const succeeded = results.filter((r) => r.status === 'fulfilled');
    expect(succeeded).toHaveLength(5);

    const { rows } = await pool.query('SELECT balance::text FROM wallets WHERE id = $1', ['src']);
    expect(rows[0].balance).toBe('500.0000');
  });

  it('prevents double-spending when balance is insufficient for all concurrent transfers', async () => {
    await seedWallets(pool, [
      { id: 'limited', balance: 150 },
      { id: 'recv1', balance: 0 },
      { id: 'recv2', balance: 0 },
      { id: 'recv3', balance: 0 },
    ]);

    // Three concurrent transfers each requesting 100; only one can succeed
    const results = await Promise.allSettled([
      service.createTransfer({
        idempotencyKey: 'spend-1',
        fromWalletId: 'limited',
        toWalletId: 'recv1',
        amount: 100,
      }),
      service.createTransfer({
        idempotencyKey: 'spend-2',
        fromWalletId: 'limited',
        toWalletId: 'recv2',
        amount: 100,
      }),
      service.createTransfer({
        idempotencyKey: 'spend-3',
        fromWalletId: 'limited',
        toWalletId: 'recv3',
        amount: 100,
      }),
    ]);

    const processed = results
      .filter(
        (r): r is PromiseFulfilledResult<Awaited<ReturnType<typeof service.createTransfer>>> =>
          r.status === 'fulfilled',
      )
      .filter((r) => r.value.transfer.status === 'PROCESSED');

    const failed = results
      .filter(
        (r): r is PromiseFulfilledResult<Awaited<ReturnType<typeof service.createTransfer>>> =>
          r.status === 'fulfilled',
      )
      .filter((r) => r.value.transfer.status === 'FAILED');

    // Balance was 150, each transfer is 100 → at most 1 can succeed
    expect(processed.length).toBeLessThanOrEqual(1);

    const { rows } = await pool.query('SELECT balance::text FROM wallets WHERE id = $1', [
      'limited',
    ]);
    const finalBalance = parseFloat(rows[0].balance);
    // Final balance must never go negative
    expect(finalBalance).toBeGreaterThanOrEqual(0);

    // Ledger must balance: total debits from 'limited' == total credits to receivers
    const { rows: ledger } = await pool.query(
      `SELECT SUM(amount)::text AS total FROM ledger_entries WHERE wallet_id = 'limited' AND type = 'DEBIT'`,
    );
    const totalDebited = parseFloat(ledger[0].total ?? '0');
    expect(1000 - finalBalance * 10).toBeGreaterThanOrEqual(0); // rough sanity
    expect(totalDebited).toBe(150 - finalBalance);

    console.log(`Concurrent spend result: ${processed.length} processed, ${failed.length} failed`);
  });

  it('safely handles transfers in both directions between the same two wallets', async () => {
    await seedWallets(pool, [
      { id: 'ping', balance: 500 },
      { id: 'pong', balance: 500 },
    ]);

    // Ping→Pong and Pong→Ping simultaneously (classic deadlock scenario without ordered locking)
    const [r1, r2] = await Promise.all([
      service.createTransfer({
        idempotencyKey: 'ping-to-pong',
        fromWalletId: 'ping',
        toWalletId: 'pong',
        amount: 100,
      }),
      service.createTransfer({
        idempotencyKey: 'pong-to-ping',
        fromWalletId: 'pong',
        toWalletId: 'ping',
        amount: 100,
      }),
    ]);

    expect(r1.transfer.status).toBe('PROCESSED');
    expect(r2.transfer.status).toBe('PROCESSED');

    const { rows } = await pool.query(
      'SELECT id, balance::text FROM wallets WHERE id = ANY($1::text[]) ORDER BY id',
      [['ping', 'pong']],
    );
    // Net result of both transfers is zero change for each wallet
    expect(rows.find((w) => w.id === 'ping')!.balance).toBe('500.0000');
    expect(rows.find((w) => w.id === 'pong')!.balance).toBe('500.0000');
  });
});
