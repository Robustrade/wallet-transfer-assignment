import { Pool, PoolClient } from 'pg';
import { Wallet } from '../domain/types';

export class WalletRepository {
  constructor(private readonly db: Pool) {}

  async findById(id: string): Promise<Wallet | null> {
    const { rows } = await this.db.query<DbWallet>(
      'SELECT id, balance::text, created_at FROM wallets WHERE id = $1',
      [id],
    );
    return rows[0] ? mapWallet(rows[0]) : null;
  }

  /**
   * Lock both wallets for the duration of the caller's transaction.
   * Ordering by id prevents deadlocks when concurrent transfers touch the same pair in reverse.
   */
  async lockForUpdate(client: PoolClient, ids: [string, string]): Promise<Wallet[]> {
    const ordered = [...ids].sort();
    const { rows } = await client.query<DbWallet>(
      'SELECT id, balance::text, created_at FROM wallets WHERE id = ANY($1::text[]) ORDER BY id FOR UPDATE',
      [ordered],
    );
    return rows.map(mapWallet);
  }

  async debit(client: PoolClient, id: string, amount: string): Promise<void> {
    await client.query('UPDATE wallets SET balance = balance - $1 WHERE id = $2', [amount, id]);
  }

  async credit(client: PoolClient, id: string, amount: string): Promise<void> {
    await client.query('UPDATE wallets SET balance = balance + $1 WHERE id = $2', [amount, id]);
  }

  async create(id: string, initialBalance = 0): Promise<Wallet> {
    const { rows } = await this.db.query<DbWallet>(
      `INSERT INTO wallets (id, balance) VALUES ($1, $2)
       ON CONFLICT (id) DO UPDATE SET id = wallets.id
       RETURNING id, balance::text, created_at`,
      [id, initialBalance],
    );
    return mapWallet(rows[0]);
  }
}

interface DbWallet {
  id: string;
  balance: string;
  created_at: Date;
}

function mapWallet(row: DbWallet): Wallet {
  return { id: row.id, balance: row.balance, createdAt: row.created_at };
}
