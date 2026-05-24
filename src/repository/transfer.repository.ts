import { Pool, PoolClient } from 'pg';
import { Transfer, TransferStatus } from '../domain/types';

interface CreateParams {
  idempotencyKey: string;
  fromWalletId: string;
  toWalletId: string;
  amount: string;
}

export class TransferRepository {
  constructor(private readonly db: Pool) {}

  async findByIdempotencyKey(key: string): Promise<Transfer | null> {
    const { rows } = await this.db.query<DbTransfer>(
      'SELECT * FROM transfers WHERE idempotency_key = $1',
      [key],
    );
    return rows[0] ? mapTransfer(rows[0]) : null;
  }

  async create(client: PoolClient, params: CreateParams): Promise<Transfer> {
    const { rows } = await client.query<DbTransfer>(
      `INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status)
       VALUES ($1, $2, $3, $4, 'PENDING')
       RETURNING *`,
      [params.idempotencyKey, params.fromWalletId, params.toWalletId, params.amount],
    );
    return mapTransfer(rows[0]);
  }

  async updateStatus(
    client: PoolClient,
    id: string,
    status: TransferStatus,
    failureReason?: string,
  ): Promise<Transfer> {
    const { rows } = await client.query<DbTransfer>(
      `UPDATE transfers
       SET status = $1, failure_reason = $2, updated_at = NOW()
       WHERE id = $3
       RETURNING *`,
      [status, failureReason ?? null, id],
    );
    return mapTransfer(rows[0]);
  }
}

interface DbTransfer {
  id: string;
  idempotency_key: string;
  from_wallet_id: string;
  to_wallet_id: string;
  amount: string;
  status: TransferStatus;
  failure_reason: string | null;
  created_at: Date;
  updated_at: Date;
}

function mapTransfer(row: DbTransfer): Transfer {
  return {
    id: row.id,
    idempotencyKey: row.idempotency_key,
    fromWalletId: row.from_wallet_id,
    toWalletId: row.to_wallet_id,
    amount: row.amount,
    status: row.status,
    failureReason: row.failure_reason,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}
