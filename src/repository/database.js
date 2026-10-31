// Connection pool and transaction management (PostgreSQL via `pg`).
import pg from 'pg';
import { SCHEMA } from './schema.js';

const INT8_OID = 20;
// BIGINT comes back from pg as a string by default. All our values are bounded
// (amounts <= 10^15), so they fit in a JS safe integer; parse them to numbers.
const types = {
  getTypeParser: (oid, format) => (oid === INT8_OID ? (v) => Number(v) : pg.types.getTypeParser(oid, format)),
};

const SCHEMA_NAME = /^[a-z_][a-z0-9_]{0,62}$/;
const SCHEMA_LOCK_KEY = 727_001; // arbitrary constant: serialises concurrent initSchema() calls

export class Database {
  #pool;

  /**
   * @param {string} connectionString  postgres://user:pass@host:port/db
   * @param {{schema?: string, max?: number, lockTimeoutMs?: number}} [options]
   *   schema: optional dedicated PostgreSQL schema (used by tests for isolation)
   */
  constructor(connectionString, { schema = null, max = 10, lockTimeoutMs = 10_000 } = {}) {
    if (schema !== null && !SCHEMA_NAME.test(schema)) throw new Error(`invalid schema name: ${schema}`);
    this.schema = schema;
    const startupOptions = [];
    if (schema) startupOptions.push(`-c search_path=${schema}`);
    // A transaction waiting for a row lock longer than this fails (-> HTTP 503) instead of hanging forever.
    startupOptions.push(`-c lock_timeout=${Number(lockTimeoutMs)}`);
    this.#pool = new pg.Pool({ connectionString, max, types, options: startupOptions.join(' ') });
    // An idle client can error (e.g. server restart); without a listener Node would crash.
    this.#pool.on('error', (err) => console.error('idle postgres client error:', err.message));
  }

  async initSchema() {
    if (this.schema) await this.#pool.query(`CREATE SCHEMA IF NOT EXISTS ${this.schema}`);
    await this.transaction(async (c) => {
      await c.query('SELECT pg_advisory_xact_lock($1)', [SCHEMA_LOCK_KEY]);
      await c.query(SCHEMA);
    });
  }

  /** Atomic unit of work (READ COMMITTED): commit on success, roll back on ANY thrown error. */
  async transaction(fn) {
    const client = await this.#pool.connect();
    let destroy = false;
    try {
      await client.query('BEGIN');
      const result = await fn(client);
      await client.query('COMMIT');
      return result;
    } catch (err) {
      try {
        await client.query('ROLLBACK');
      } catch {
        destroy = true; // connection is in an unknown state: do not return it to the pool
      }
      throw err;
    } finally {
      client.release(destroy);
    }
  }

  /** Removes all data (dev/seed only). TRUNCATE is not affected by the row-level ledger triggers. */
  async truncateAll() {
    await this.#pool.query('TRUNCATE ledger_entries, idempotency_records, transfers, wallets');
  }

  /** Drops the dedicated schema (tests). */
  async dropSchema() {
    if (!this.schema) throw new Error('refusing to drop: no dedicated schema configured');
    await this.#pool.query(`DROP SCHEMA IF EXISTS ${this.schema} CASCADE`);
  }

  async close() {
    await this.#pool.end();
  }
}
