// Seeds sample data. Safe to re-run: existing wallets are skipped and sample transfers use fixed
// idempotency keys, so they are applied exactly once (a nice live demo of idempotency).
//
//   npm run seed          add sample wallets + transfers if missing
//   npm run seed:reset    wipe ALL data first (refused when NODE_ENV=production)
import { DEFAULT_DATABASE_URL } from '../src/main.js';
import { Database } from '../src/repository/index.js';
import { TransferService } from '../src/service/index.js';

const WALLETS = [
  { id: 'wallet_1', initialBalance: 500 },
  { id: 'wallet_2', initialBalance: 0 },
  { id: 'wallet_3', initialBalance: 1000 },
];

const TRANSFERS = [
  { idempotencyKey: 'seed-transfer-1', fromWalletId: 'wallet_3', toWalletId: 'wallet_1', amount: 250 },
  { idempotencyKey: 'seed-transfer-2', fromWalletId: 'wallet_1', toWalletId: 'wallet_2', amount: 100 },
];

const reset = process.argv.includes('--reset');
if (reset && process.env.NODE_ENV === 'production') {
  console.error('refusing to reset data when NODE_ENV=production');
  process.exit(1);
}

const db = new Database(process.env.DATABASE_URL ?? DEFAULT_DATABASE_URL);
const service = new TransferService(db);

try {
  await db.initSchema();
  if (reset) {
    await db.truncateAll();
    console.log('all data removed');
  }

  for (const { id, initialBalance } of WALLETS) {
    try {
      await service.createWallet(id, initialBalance);
      console.log(`created wallet ${id} (balance ${initialBalance})`);
    } catch (err) {
      if (err.code !== 'WALLET_ALREADY_EXISTS') throw err;
      console.log(`skipped wallet ${id} (already exists)`);
    }
  }

  for (const request of TRANSFERS) {
    const { transfer, replayed } = await service.createTransfer(request);
    console.log(`${replayed ? 'skipped' : 'created'} transfer ${request.idempotencyKey}: `
      + `${request.fromWalletId} -> ${request.toWalletId} ${request.amount} (${transfer.status})`);
  }

  for (const { id } of WALLETS) console.log(`balance ${id}: ${(await service.getWallet(id)).balance}`);
} finally {
  await db.close();
}
