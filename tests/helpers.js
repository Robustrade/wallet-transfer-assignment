import { randomBytes } from 'node:crypto';
import assert from 'node:assert/strict';
import { Database, checkIntegrity } from '../src/repository/index.js';
import { TransferService } from '../src/service/index.js';

export const DATABASE_URL = process.env.DATABASE_URL ?? 'postgres://wallet:wallet@localhost:5432/wallet';

/**
 * A real PostgreSQL database (no mocking), isolated per test through a throwaway schema,
 * so test files can safely run in parallel against the same server.
 */
export async function createFixture() {
  const schema = `test_${randomBytes(6).toString('hex')}`;
  const db = new Database(DATABASE_URL, { schema, max: 20 });
  try {
    await db.initSchema();
  } catch (err) {
    await db.close().catch(() => {});
    throw new Error(
      `Cannot reach PostgreSQL at ${DATABASE_URL.replace(/:[^:@/]*@/, ':***@')} (${err.code ?? err.message}).\n`
      + 'Start it with `npm run db:up` (Docker) or set DATABASE_URL.',
    );
  }
  const service = new TransferService(db);

  return {
    db, service,
    wallet: (id, balance = 0) => service.createWallet(id, balance),
    balance: async (id) => (await service.getWallet(id)).balance,
    count: (table) => db.transaction(async (c) => (await c.query(`SELECT COUNT(*) AS n FROM ${table}`)).rows[0].n),
    assertIntegrity: async () => assert.deepEqual(await db.transaction((c) => checkIntegrity(c)), []),
    async cleanup() {
      await db.dropSchema();
      await db.close();
    },
  };
}
