import { Pool, PoolClient } from 'pg';
import { LedgerEntry } from '../domain/types';

interface CreateEntriesParams {
  transferId: string;
  fromWalletId: string;
  toWalletId: string;
  amount: string;
}

export class LedgerRepository {
  constructor(private readonly db: Pool) {}

  /**
   * Insert the DEBIT (source) and CREDIT (destination) entries atomically.
   * A single multi-row INSERT ensures both rows land in the same WAL record.
   */
  async createEntries(client: PoolClient, params: CreateEntriesParams): Promise<LedgerEntry[]> {
    const { rows } = await client.query<DbLedgerEntry>(
      `INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount)
       VALUES ($1, $2, 'DEBIT', $3),
              ($4, $2, 'CREDIT', $3)
       RETURNING *`,
      [params.fromWalletId, params.transferId, params.amount, params.toWalletId],
    );
    return rows.map(mapEntry);
  }

  async findByWalletId(walletId: string): Promise<LedgerEntry[]> {
    const { rows } = await this.db.query<DbLedgerEntry>(
      'SELECT * FROM ledger_entries WHERE wallet_id = $1 ORDER BY created_at',
      [walletId],
    );
    return rows.map(mapEntry);
  }

  async findByTransferId(client: Pool | PoolClient, transferId: string): Promise<LedgerEntry[]> {
    const { rows } = await client.query<DbLedgerEntry>(
      'SELECT * FROM ledger_entries WHERE transfer_id = $1 ORDER BY created_at',
      [transferId],
    );
    return rows.map(mapEntry);
  }
}

interface DbLedgerEntry {
  id: string;
  wallet_id: string;
  transfer_id: string;
  type: 'DEBIT' | 'CREDIT';
  amount: string;
  created_at: Date;
}

function mapEntry(row: DbLedgerEntry): LedgerEntry {
  return {
    id: row.id,
    walletId: row.wallet_id,
    transferId: row.transfer_id,
    type: row.type,
    amount: row.amount,
    createdAt: row.created_at,
  };
}
