import { Pool } from 'pg';
import { runMigrations } from '../../src/db/migrate';

const TEST_DATABASE_URL =
  process.env.TEST_DATABASE_URL || 'postgresql://wallet:wallet@localhost:5433/wallet_transfer_test';

export function createTestPool(): Pool {
  return new Pool({ connectionString: TEST_DATABASE_URL });
}

export async function setupTestDb(pool: Pool): Promise<void> {
  // Drop and recreate tables for a clean slate each suite
  await pool.query(`
    DROP TABLE IF EXISTS ledger_entries CASCADE;
    DROP TABLE IF EXISTS transfers CASCADE;
    DROP TABLE IF EXISTS wallets CASCADE;
  `);
  await runMigrations(pool);
}

export async function teardownTestDb(pool: Pool): Promise<void> {
  await pool.end();
}

export async function seedWallets(
  pool: Pool,
  wallets: Array<{ id: string; balance: number }>,
): Promise<void> {
  for (const w of wallets) {
    await pool.query('INSERT INTO wallets (id, balance) VALUES ($1, $2)', [w.id, w.balance]);
  }
}
