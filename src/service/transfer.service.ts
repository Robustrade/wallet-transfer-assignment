import { Pool } from 'pg';
import { WalletRepository } from '../repository/wallet.repository';
import { TransferRepository } from '../repository/transfer.repository';
import { LedgerRepository } from '../repository/ledger.repository';
import { CreateTransferInput, TransferResult } from '../domain/types';
import { WalletNotFoundError } from '../domain/errors';

export class TransferService {
  constructor(
    private readonly pool: Pool,
    private readonly wallets: WalletRepository,
    private readonly transfers: TransferRepository,
    private readonly ledger: LedgerRepository,
  ) {}

  async createTransfer(input: CreateTransferInput): Promise<TransferResult> {
    // Quick pre-check outside the transaction: avoids acquiring locks on obvious duplicates.
    const existing = await this.transfers.findByIdempotencyKey(input.idempotencyKey);
    if (existing) {
      const ledgerEntries = await this.ledger.findByTransferId(this.pool, existing.id);
      return { transfer: existing, ledgerEntries, duplicate: true };
    }

    const client = await this.pool.connect();
    try {
      await client.query('BEGIN');

      // Lock both wallets in a deterministic order (sorted by id) to prevent deadlocks
      // when concurrent transfers involve the same wallet pair in opposite directions.
      const lockedWallets = await this.wallets.lockForUpdate(client, [
        input.fromWalletId,
        input.toWalletId,
      ]);

      if (lockedWallets.length < 2) {
        const foundIds = new Set(lockedWallets.map((w) => w.id));
        const missing = [input.fromWalletId, input.toWalletId].filter((id) => !foundIds.has(id));
        throw new WalletNotFoundError(missing);
      }

      const fromWallet = lockedWallets.find((w) => w.id === input.fromWalletId)!;
      const amount = input.amount.toFixed(4);

      // Try to insert the transfer record. The UNIQUE constraint on idempotency_key
      // is the safety net: if a concurrent request with the same key already committed,
      // PostgreSQL will throw a 23505 error and we fall back to returning that record.
      let transfer;
      try {
        transfer = await this.transfers.create(client, {
          idempotencyKey: input.idempotencyKey,
          fromWalletId: input.fromWalletId,
          toWalletId: input.toWalletId,
          amount,
        });
      } catch (err: unknown) {
        if (isUniqueViolation(err)) {
          await client.query('ROLLBACK');
          const concurrent = await this.transfers.findByIdempotencyKey(input.idempotencyKey);
          const ledgerEntries = concurrent
            ? await this.ledger.findByTransferId(this.pool, concurrent.id)
            : [];
          return { transfer: concurrent!, ledgerEntries, duplicate: true };
        }
        throw err;
      }

      // Balance check happens after the lock so no concurrent debit can sneak in.
      if (parseFloat(fromWallet.balance) < input.amount) {
        const failed = await this.transfers.updateStatus(
          client,
          transfer.id,
          'FAILED',
          'Insufficient balance',
        );
        await client.query('COMMIT');
        return { transfer: failed, ledgerEntries: [], duplicate: false };
      }

      // Double-entry ledger: DEBIT source, CREDIT destination
      const ledgerEntries = await this.ledger.createEntries(client, {
        transferId: transfer.id,
        fromWalletId: input.fromWalletId,
        toWalletId: input.toWalletId,
        amount,
      });

      // Update stored balances
      await this.wallets.debit(client, input.fromWalletId, amount);
      await this.wallets.credit(client, input.toWalletId, amount);

      const processed = await this.transfers.updateStatus(client, transfer.id, 'PROCESSED');

      await client.query('COMMIT');
      return { transfer: processed, ledgerEntries, duplicate: false };
    } catch (err) {
      await client.query('ROLLBACK').catch(() => undefined);
      throw err;
    } finally {
      client.release();
    }
  }
}

function isUniqueViolation(err: unknown): boolean {
  return typeof err === 'object' && err !== null && (err as { code?: string }).code === '23505';
}
