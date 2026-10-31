// Persistence operations. No business rules live here; each class wraps one transaction's client.
import {
  EntryType, InvalidStateTransition, Transfer, TransferStatus, WalletAlreadyExists, WalletNotFound,
} from '../domain/index.js';

const UNIQUE_VIOLATION = '23505';
const iso = (d) => (d instanceof Date ? d.toISOString() : d);

const mapWallet = (r) => ({ id: r.id, balance: r.balance, initialBalance: r.initial_balance, createdAt: iso(r.created_at) });

export class WalletRepository {
  constructor(client) { this.c = client; }

  async create(walletId, initialBalance, now) {
    try {
      await this.c.query(
        'INSERT INTO wallets (id, initial_balance, balance, created_at) VALUES ($1, $2, $2, $3)',
        [walletId, initialBalance, now],
      );
    } catch (err) {
      if (err.code === UNIQUE_VIOLATION) throw new WalletAlreadyExists(`wallet ${walletId} already exists`);
      throw err;
    }
    return { id: walletId, balance: initialBalance, initialBalance, createdAt: now };
  }

  async get(walletId) {
    const { rows } = await this.c.query('SELECT * FROM wallets WHERE id = $1', [walletId]);
    if (rows.length === 0) throw new WalletNotFound(`wallet ${walletId} not found`);
    return mapWallet(rows[0]);
  }

  /**
   * Locks BOTH wallet rows (SELECT ... FOR UPDATE) in sorted id order.
   * Always locking in the same global order means two opposite transfers (A->B and B->A)
   * can never wait on each other, so no deadlock is possible.
   */
  async lockPair(fromId, toId) {
    const { rows } = await this.c.query(
      'SELECT * FROM wallets WHERE id = ANY($1::text[]) ORDER BY id FOR UPDATE',
      [[fromId, toId]],
    );
    const found = new Map(rows.map((r) => [r.id, mapWallet(r)]));
    for (const id of [fromId, toId]) {
      if (!found.has(id)) throw new WalletNotFound(`wallet ${id} not found`);
    }
    return { from: found.get(fromId), to: found.get(toId) };
  }

  /**
   * Atomic guarded decrement. Returns false (and changes nothing) when funds are insufficient.
   * The guard is in the UPDATE itself, so it stays correct even without the row lock.
   */
  async debit(walletId, amount) {
    const { rowCount } = await this.c.query(
      'UPDATE wallets SET balance = balance - $1 WHERE id = $2 AND balance >= $1', [amount, walletId],
    );
    return rowCount === 1;
  }

  async credit(walletId, amount) {
    const { rowCount } = await this.c.query('UPDATE wallets SET balance = balance + $1 WHERE id = $2', [amount, walletId]);
    if (rowCount !== 1) throw new WalletNotFound(`wallet ${walletId} not found`);
  }
}

export class TransferRepository {
  constructor(client) { this.c = client; }

  static map(r) {
    return new Transfer({
      id: r.id, idempotencyKey: r.idempotency_key, fromWalletId: r.from_wallet_id, toWalletId: r.to_wallet_id,
      amount: r.amount, status: r.status, failureReason: r.failure_reason,
      createdAt: iso(r.created_at), updatedAt: iso(r.updated_at),
    });
  }

  async insert(t) {
    await this.c.query(
      `INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount,
                              status, failure_reason, created_at, updated_at)
       VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
      [t.id, t.idempotencyKey, t.fromWalletId, t.toWalletId, t.amount, t.status, t.failureReason, t.createdAt, t.updatedAt],
    );
  }

  async get(transferId) {
    const { rows } = await this.c.query('SELECT * FROM transfers WHERE id = $1', [transferId]);
    return rows.length ? TransferRepository.map(rows[0]) : null;
  }

  /** Compare-and-set on status; a lost race or illegal move throws instead of silently passing. */
  async applyTransition(expectedFrom, updated) {
    const { rowCount } = await this.c.query(
      'UPDATE transfers SET status = $1, failure_reason = $2, updated_at = $3 WHERE id = $4 AND status = $5',
      [updated.status, updated.failureReason, updated.updatedAt, updated.id, expectedFrom],
    );
    if (rowCount !== 1) throw new InvalidStateTransition(`transfer ${updated.id} was not in state ${expectedFrom}`);
  }
}

const mapEntry = (r) => ({
  id: r.id, walletId: r.wallet_id, transferId: r.transfer_id, type: r.type, amount: r.amount, createdAt: iso(r.created_at),
});

export class LedgerRepository {
  constructor(client) { this.c = client; }

  async addPair({ transferId, fromWalletId, toWalletId, amount, now }) {
    const sql = 'INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount, created_at) VALUES ($1,$2,$3,$4,$5)';
    await this.c.query(sql, [fromWalletId, transferId, EntryType.DEBIT, amount, now]);
    await this.c.query(sql, [toWalletId, transferId, EntryType.CREDIT, amount, now]);
  }

  async listForWallet(walletId) {
    const { rows } = await this.c.query('SELECT * FROM ledger_entries WHERE wallet_id = $1 ORDER BY id', [walletId]);
    return rows.map(mapEntry);
  }

  async listForTransfer(transferId) {
    const { rows } = await this.c.query('SELECT * FROM ledger_entries WHERE transfer_id = $1 ORDER BY id', [transferId]);
    return rows.map(mapEntry);
  }
}

export class IdempotencyRepository {
  constructor(client) { this.c = client; }

  /**
   * Transaction-scoped advisory lock on the key: concurrent requests carrying the SAME key are
   * serialised, so "look up -> not found -> execute" cannot be done twice. Released automatically
   * at COMMIT/ROLLBACK. Lock order is always advisory-lock -> wallet rows, so it cannot deadlock.
   */
  async lock(key) {
    await this.c.query('SELECT pg_advisory_xact_lock(hashtextextended($1, 0))', [key]);
  }

  async get(key) {
    const { rows } = await this.c.query('SELECT key, request_hash, transfer_id FROM idempotency_records WHERE key = $1', [key]);
    return rows.length ? { key: rows[0].key, requestHash: rows[0].request_hash, transferId: rows[0].transfer_id } : null;
  }

  async insert(key, requestHash, transferId, now) {
    await this.c.query(
      'INSERT INTO idempotency_records (key, request_hash, transfer_id, created_at) VALUES ($1,$2,$3,$4)',
      [key, requestHash, transferId, now],
    );
  }
}

/** Returns a list of violated invariants (empty array == healthy). Used by tests / ops. */
export async function checkIntegrity(client) {
  const problems = [];

  const wallets = await client.query(`
    SELECT w.id, w.balance, w.initial_balance,
           COALESCE(SUM(l.amount) FILTER (WHERE l.type = 'CREDIT'), 0)::bigint AS credits,
           COALESCE(SUM(l.amount) FILTER (WHERE l.type = 'DEBIT'),  0)::bigint AS debits
    FROM wallets w LEFT JOIN ledger_entries l ON l.wallet_id = w.id
    GROUP BY w.id`);
  for (const r of wallets.rows) {
    const expected = r.initial_balance + r.credits - r.debits;
    if (r.balance !== expected) problems.push(`wallet ${r.id}: balance ${r.balance} != ledger-derived ${expected}`);
    if (r.balance < 0) problems.push(`wallet ${r.id}: negative balance`);
  }

  const { rows: [totals] } = await client.query(`
    SELECT COALESCE(SUM(amount) FILTER (WHERE type = 'DEBIT'),  0)::bigint AS d,
           COALESCE(SUM(amount) FILTER (WHERE type = 'CREDIT'), 0)::bigint AS c
    FROM ledger_entries`);
  if (totals.d !== totals.c) problems.push(`ledger unbalanced: debits ${totals.d} != credits ${totals.c}`);

  const transfers = await client.query(`
    SELECT t.id, t.status,
           COUNT(*) FILTER (WHERE l.type = 'DEBIT'  AND l.amount = t.amount AND l.wallet_id = t.from_wallet_id) AS good_debits,
           COUNT(*) FILTER (WHERE l.type = 'CREDIT' AND l.amount = t.amount AND l.wallet_id = t.to_wallet_id)   AS good_credits,
           COUNT(l.id) AS entries
    FROM transfers t LEFT JOIN ledger_entries l ON l.transfer_id = t.id
    GROUP BY t.id`);
  for (const r of transfers.rows) {
    if (r.status === TransferStatus.PENDING) {
      problems.push(`transfer ${r.id}: committed in PENDING state`);
    } else if (r.status === TransferStatus.PROCESSED) {
      if (!(r.entries === 2 && r.good_debits === 1 && r.good_credits === 1)) {
        problems.push(`transfer ${r.id}: PROCESSED without a correct debit/credit pair`);
      }
    } else if (r.entries !== 0) {
      problems.push(`transfer ${r.id}: FAILED but has ledger entries`);
    }
  }
  return problems;
}
