import { Pool } from 'pg';
import request from 'supertest';
import { createApp } from '../src/app';
import { createTestPool, setupTestDb, teardownTestDb, seedWallets } from './helpers/db';

let pool: Pool;
// eslint-disable-next-line @typescript-eslint/no-explicit-any
let app: any;

beforeAll(async () => {
  pool = createTestPool();
  await setupTestDb(pool);
  app = createApp(pool);
});

afterAll(() => teardownTestDb(pool));

beforeEach(async () => {
  await pool.query('DELETE FROM ledger_entries');
  await pool.query('DELETE FROM transfers');
  await pool.query('DELETE FROM wallets');
  await seedWallets(pool, [
    { id: 'h-wallet-a', balance: 500 },
    { id: 'h-wallet-b', balance: 200 },
  ]);
});

describe('POST /transfers', () => {
  it('returns 201 on a successful transfer', async () => {
    const res = await request(app).post('/transfers').send({
      idempotencyKey: 'h-1',
      fromWalletId: 'h-wallet-a',
      toWalletId: 'h-wallet-b',
      amount: 100,
    });

    expect(res.status).toBe(201);
    expect(res.body.transfer.status).toBe('PROCESSED');
    expect(res.body.ledgerEntries).toHaveLength(2);
  });

  it('returns 200 on a duplicate request', async () => {
    const body = {
      idempotencyKey: 'h-dup',
      fromWalletId: 'h-wallet-a',
      toWalletId: 'h-wallet-b',
      amount: 50,
    };
    await request(app).post('/transfers').send(body);
    const res = await request(app).post('/transfers').send(body);

    expect(res.status).toBe(200);
    expect(res.body.duplicate).toBe(true);
  });

  it('returns 422 when balance is insufficient', async () => {
    const res = await request(app).post('/transfers').send({
      idempotencyKey: 'h-insuf',
      fromWalletId: 'h-wallet-b',
      toWalletId: 'h-wallet-a',
      amount: 9999,
    });

    expect(res.status).toBe(422);
    expect(res.body.transfer.status).toBe('FAILED');
  });

  it('returns 400 when idempotencyKey is missing', async () => {
    const res = await request(app).post('/transfers').send({
      fromWalletId: 'h-wallet-a',
      toWalletId: 'h-wallet-b',
      amount: 100,
    });
    expect(res.status).toBe(400);
  });

  it('returns 400 when amount is negative', async () => {
    const res = await request(app).post('/transfers').send({
      idempotencyKey: 'h-neg',
      fromWalletId: 'h-wallet-a',
      toWalletId: 'h-wallet-b',
      amount: -50,
    });
    expect(res.status).toBe(400);
  });

  it('returns 404 when a wallet does not exist', async () => {
    const res = await request(app).post('/transfers').send({
      idempotencyKey: 'h-nf',
      fromWalletId: 'ghost-wallet',
      toWalletId: 'h-wallet-b',
      amount: 10,
    });
    expect(res.status).toBe(404);
  });
});

describe('GET /wallets/:id', () => {
  it('returns wallet with balance', async () => {
    const res = await request(app).get('/wallets/h-wallet-a');
    expect(res.status).toBe(200);
    expect(res.body.id).toBe('h-wallet-a');
    expect(parseFloat(res.body.balance)).toBe(500);
  });

  it('returns 404 for unknown wallet', async () => {
    const res = await request(app).get('/wallets/ghost');
    expect(res.status).toBe(404);
  });
});

describe('GET /wallets/:id/ledger', () => {
  it('returns ledger entries after a transfer', async () => {
    await request(app).post('/transfers').send({
      idempotencyKey: 'h-ledger',
      fromWalletId: 'h-wallet-a',
      toWalletId: 'h-wallet-b',
      amount: 75,
    });

    const res = await request(app).get('/wallets/h-wallet-a/ledger');
    expect(res.status).toBe(200);
    expect(res.body).toHaveLength(1);
    expect(res.body[0].type).toBe('DEBIT');
    expect(parseFloat(res.body[0].amount)).toBe(75);
  });
});
