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
  await seedWallets(pool, [
    { id: 'w1', balance: 1000 },
    { id: 'w2', balance: 0 },
  ]);
});

describe('idempotency', () => {
  it('returns the same transfer on a duplicate request', async () => {
    const input = { idempotencyKey: 'idem-1', fromWalletId: 'w1', toWalletId: 'w2', amount: 100 };

    const first = await service.createTransfer(input);
    const second = await service.createTransfer(input);

    expect(second.duplicate).toBe(true);
    expect(second.transfer.id).toBe(first.transfer.id);
    expect(second.transfer.status).toBe(first.transfer.status);
  });

  it('does not apply the transfer twice when the same key is used twice', async () => {
    const input = { idempotencyKey: 'idem-2', fromWalletId: 'w1', toWalletId: 'w2', amount: 300 };

    await service.createTransfer(input);
    await service.createTransfer(input);

    const { rows } = await pool.query('SELECT balance::text FROM wallets WHERE id = $1', ['w1']);
    // Balance should only drop once (1000 - 300 = 700), not twice
    expect(rows[0].balance).toBe('700.0000');
  });

  it('returns the same ledger entries on a duplicate request', async () => {
    const input = { idempotencyKey: 'idem-3', fromWalletId: 'w1', toWalletId: 'w2', amount: 50 };

    const first = await service.createTransfer(input);
    const second = await service.createTransfer(input);

    expect(second.ledgerEntries).toHaveLength(2);
    expect(second.ledgerEntries.map((e) => e.id).sort()).toEqual(
      first.ledgerEntries.map((e) => e.id).sort(),
    );
  });

  it('stores only one transfer record in the database for duplicate requests', async () => {
    const input = { idempotencyKey: 'idem-4', fromWalletId: 'w1', toWalletId: 'w2', amount: 10 };

    await service.createTransfer(input);
    await service.createTransfer(input);
    await service.createTransfer(input);

    const { rows } = await pool.query(
      'SELECT COUNT(*) AS cnt FROM transfers WHERE idempotency_key = $1',
      ['idem-4'],
    );
    expect(parseInt(rows[0].cnt, 10)).toBe(1);
  });

  it('returns the FAILED result on a duplicate of a failed transfer', async () => {
    const input = {
      idempotencyKey: 'idem-fail',
      fromWalletId: 'w1',
      toWalletId: 'w2',
      amount: 99999,
    };

    const first = await service.createTransfer(input);
    expect(first.transfer.status).toBe('FAILED');

    const second = await service.createTransfer(input);
    expect(second.duplicate).toBe(true);
    expect(second.transfer.status).toBe('FAILED');
    expect(second.transfer.id).toBe(first.transfer.id);
  });
});
