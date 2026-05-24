import { Pool } from 'pg';
import { TransferService } from '../src/service/transfer.service';
import { WalletRepository } from '../src/repository/wallet.repository';
import { TransferRepository } from '../src/repository/transfer.repository';
import { LedgerRepository } from '../src/repository/ledger.repository';
import { WalletNotFoundError } from '../src/domain/errors';
import { createTestPool, setupTestDb, teardownTestDb, seedWallets } from './helpers/db';

let pool: Pool;
let service: TransferService;

beforeAll(async () => {
  pool = createTestPool();
  await setupTestDb(pool);
  const walletRepo = new WalletRepository(pool);
  const transferRepo = new TransferRepository(pool);
  const ledgerRepo = new LedgerRepository(pool);
  service = new TransferService(pool, walletRepo, transferRepo, ledgerRepo);
});

afterAll(() => teardownTestDb(pool));

beforeEach(async () => {
  await pool.query('DELETE FROM ledger_entries');
  await pool.query('DELETE FROM transfers');
  await pool.query('DELETE FROM wallets');
  await seedWallets(pool, [
    { id: 'wallet-a', balance: 1000 },
    { id: 'wallet-b', balance: 500 },
  ]);
});

describe('createTransfer — happy path', () => {
  it('debits source wallet and credits destination wallet', async () => {
    await service.createTransfer({
      idempotencyKey: 'key-1',
      fromWalletId: 'wallet-a',
      toWalletId: 'wallet-b',
      amount: 200,
    });

    const { rows: wallets } = await pool.query(
      'SELECT id, balance::text FROM wallets WHERE id = ANY($1::text[]) ORDER BY id',
      [['wallet-a', 'wallet-b']],
    );
    expect(wallets.find((w) => w.id === 'wallet-a')!.balance).toBe('800.0000');
    expect(wallets.find((w) => w.id === 'wallet-b')!.balance).toBe('700.0000');
  });

  it('returns a PROCESSED transfer', async () => {
    const { transfer } = await service.createTransfer({
      idempotencyKey: 'key-2',
      fromWalletId: 'wallet-a',
      toWalletId: 'wallet-b',
      amount: 100,
    });
    expect(transfer.status).toBe('PROCESSED');
    expect(transfer.idempotencyKey).toBe('key-2');
  });

  it('creates exactly two ledger entries per transfer', async () => {
    const { transfer, ledgerEntries } = await service.createTransfer({
      idempotencyKey: 'key-3',
      fromWalletId: 'wallet-a',
      toWalletId: 'wallet-b',
      amount: 50,
    });

    expect(ledgerEntries).toHaveLength(2);
    const debit = ledgerEntries.find((e) => e.type === 'DEBIT')!;
    const credit = ledgerEntries.find((e) => e.type === 'CREDIT')!;
    expect(debit.walletId).toBe('wallet-a');
    expect(credit.walletId).toBe('wallet-b');
    expect(debit.amount).toBe('50.0000');
    expect(credit.amount).toBe('50.0000');
    expect(debit.transferId).toBe(transfer.id);
    expect(credit.transferId).toBe(transfer.id);
  });
});

describe('createTransfer — insufficient balance', () => {
  it('marks transfer as FAILED when source wallet has insufficient funds', async () => {
    const { transfer, ledgerEntries } = await service.createTransfer({
      idempotencyKey: 'key-insuf',
      fromWalletId: 'wallet-b',
      toWalletId: 'wallet-a',
      amount: 1000,
    });

    expect(transfer.status).toBe('FAILED');
    expect(transfer.failureReason).toMatch(/insufficient/i);
    expect(ledgerEntries).toHaveLength(0);
  });

  it('does not change balances when transfer fails', async () => {
    await service.createTransfer({
      idempotencyKey: 'key-insuf-2',
      fromWalletId: 'wallet-b',
      toWalletId: 'wallet-a',
      amount: 9999,
    });

    const { rows } = await pool.query(
      'SELECT id, balance::text FROM wallets WHERE id = ANY($1::text[]) ORDER BY id',
      [['wallet-a', 'wallet-b']],
    );
    expect(rows.find((w) => w.id === 'wallet-a')!.balance).toBe('1000.0000');
    expect(rows.find((w) => w.id === 'wallet-b')!.balance).toBe('500.0000');
  });

  it('rejects on exact balance boundary', async () => {
    // wallet-b has 500; transferring exactly 500 should succeed
    const { transfer } = await service.createTransfer({
      idempotencyKey: 'key-boundary',
      fromWalletId: 'wallet-b',
      toWalletId: 'wallet-a',
      amount: 500,
    });
    expect(transfer.status).toBe('PROCESSED');
  });
});

describe('createTransfer — wallet not found', () => {
  it('throws WalletNotFoundError when source wallet does not exist', async () => {
    await expect(
      service.createTransfer({
        idempotencyKey: 'key-nf-1',
        fromWalletId: 'wallet-ghost',
        toWalletId: 'wallet-b',
        amount: 100,
      }),
    ).rejects.toBeInstanceOf(WalletNotFoundError);
  });

  it('throws WalletNotFoundError when destination wallet does not exist', async () => {
    await expect(
      service.createTransfer({
        idempotencyKey: 'key-nf-2',
        fromWalletId: 'wallet-a',
        toWalletId: 'wallet-ghost',
        amount: 100,
      }),
    ).rejects.toBeInstanceOf(WalletNotFoundError);
  });
});
