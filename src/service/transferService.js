// Business logic: transfer workflow, idempotency and wallet operations.
import {
  INSUFFICIENT_FUNDS, MAX_AMOUNT, IdempotencyKeyConflict, Transfer, TransferNotFound,
  TransferStatus, ValidationError, requestFingerprint, validateTransferRequest, validateWalletId,
} from '../domain/index.js';
import {
  IdempotencyRepository, LedgerRepository, TransferRepository, WalletRepository,
} from '../repository/index.js';

export const utcNow = () => new Date().toISOString();

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const noopLogger = { info() {} };

export class TransferService {
  /**
   * @param {import('../repository/database.js').Database} db
   * @param {{clock?: () => string, logger?: {info: (line: string) => void}}} [options]
   */
  constructor(db, { clock = utcNow, logger = noopLogger } = {}) {
    this.db = db;
    this.clock = clock;
    this.logger = logger;
  }

  // ------------------------------------------------------------------ transfers
  /** @returns {Promise<{transfer: Transfer, replayed: boolean}>} */
  async createTransfer({ fromWalletId, toWalletId, amount, idempotencyKey = null }) {
    const started = performance.now();
    try {
      validateTransferRequest({ fromWalletId, toWalletId, amount, idempotencyKey });
    } catch (err) {
      this.#log('transfer_rejected', { key: idempotencyKey, reason: err.code, started });
      throw err;
    }
    const fingerprint = requestFingerprint(fromWalletId, toWalletId, amount);

    // One atomic unit of work: idempotency check, balance change, ledger rows,
    // state transition and idempotency record commit together or not at all.
    const result = await this.db.transaction(async (conn) => {
      const wallets = new WalletRepository(conn);
      const transfers = new TransferRepository(conn);
      const ledger = new LedgerRepository(conn);
      const idem = new IdempotencyRepository(conn);

      if (idempotencyKey !== null) {
        await idem.lock(idempotencyKey); // serialise concurrent requests with the same key
        const record = await idem.get(idempotencyKey);
        if (record) {
          if (record.requestHash !== fingerprint) {
            this.#log('transfer_rejected', { key: idempotencyKey, reason: 'IDEMPOTENCY_KEY_CONFLICT', started });
            throw new IdempotencyKeyConflict('idempotencyKey was already used with a different request');
          }
          return { transfer: await transfers.get(record.transferId), replayed: true };
        }
      }

      // Lock both wallet rows in sorted order (deadlock-free). Also verifies they exist,
      // before anything is written, so an unknown wallet leaves no trace.
      await wallets.lockPair(fromWalletId, toWalletId);

      const now = this.clock();
      const pending = Transfer.newPending({ fromWalletId, toWalletId, amount, idempotencyKey, now });
      await transfers.insert(pending);
      if (idempotencyKey !== null) await idem.insert(idempotencyKey, fingerprint, pending.id, now);

      let final;
      if (await wallets.debit(fromWalletId, amount)) {
        await wallets.credit(toWalletId, amount);
        await ledger.addPair({ transferId: pending.id, fromWalletId, toWalletId, amount, now });
        final = pending.transitionTo(TransferStatus.PROCESSED, now);
      } else {
        final = pending.transitionTo(TransferStatus.FAILED, now, INSUFFICIENT_FUNDS);
      }
      await transfers.applyTransition(TransferStatus.PENDING, final);
      return { transfer: final, replayed: false };
    });

    const event = result.replayed ? 'transfer_replayed'
      : result.transfer.status === TransferStatus.PROCESSED ? 'transfer_processed' : 'transfer_failed';
    this.#log(event, { transfer: result.transfer, started });
    return result;
  }

  async getTransfer(transferId) {
    // A malformed id can never exist; avoids PostgreSQL's "invalid input syntax for type uuid" -> 500.
    const transfer = UUID.test(transferId)
      ? await this.db.transaction((conn) => new TransferRepository(conn).get(transferId))
      : null;
    if (!transfer) throw new TransferNotFound(`transfer ${transferId} not found`);
    return transfer;
  }

  // ------------------------------------------------------------------ wallets
  async createWallet(walletId, initialBalance = 0) {
    validateWalletId(walletId, 'id');
    if (typeof initialBalance !== 'number' || !Number.isSafeInteger(initialBalance) || initialBalance < 0) {
      throw new ValidationError('initialBalance must be a non-negative integer');
    }
    if (initialBalance > MAX_AMOUNT) throw new ValidationError(`initialBalance must be at most ${MAX_AMOUNT}`);
    return this.db.transaction((conn) => new WalletRepository(conn).create(walletId, initialBalance, this.clock()));
  }

  async getWallet(walletId) {
    return this.db.transaction((conn) => new WalletRepository(conn).get(walletId));
  }

  async getLedger(walletId) {
    return this.db.transaction(async (conn) => {
      await new WalletRepository(conn).get(walletId); // 404 for unknown wallets
      return new LedgerRepository(conn).listForWallet(walletId);
    });
  }

  // ------------------------------------------------------------------ logging
  #log(event, { transfer = null, key = null, reason = null, started }) {
    const parts = [`event=${event}`];
    if (transfer) {
      parts.push(`transfer_id=${transfer.id}`, `idempotency_key=${transfer.idempotencyKey}`,
        `from=${transfer.fromWalletId}`, `to=${transfer.toWalletId}`, `amount=${transfer.amount}`,
        `status=${transfer.status}`);
      if (transfer.failureReason) parts.push(`reason=${transfer.failureReason}`);
    } else {
      parts.push(`idempotency_key=${key}`);
      if (reason) parts.push(`reason=${reason}`);
    }
    parts.push(`duration_ms=${(performance.now() - started).toFixed(1)}`);
    this.logger.info(parts.join(' '));
  }
}
