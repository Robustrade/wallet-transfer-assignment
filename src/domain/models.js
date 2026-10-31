// Domain entities, state machine and validation rules (no I/O).
import { createHash, randomUUID } from 'node:crypto';
import { InvalidStateTransition, ValidationError } from './errors.js';

export const MAX_AMOUNT = 10 ** 15;
export const MAX_ID_LENGTH = 128;
export const MAX_KEY_LENGTH = 255;
export const INSUFFICIENT_FUNDS = 'INSUFFICIENT_FUNDS';

export const TransferStatus = Object.freeze({
  PENDING: 'PENDING',
  PROCESSED: 'PROCESSED',
  FAILED: 'FAILED',
});

export const EntryType = Object.freeze({ DEBIT: 'DEBIT', CREDIT: 'CREDIT' });

const ALLOWED_TRANSITIONS = Object.freeze({
  [TransferStatus.PENDING]: [TransferStatus.PROCESSED, TransferStatus.FAILED],
  [TransferStatus.PROCESSED]: [],
  [TransferStatus.FAILED]: [],
});

/** Immutable: transitions return a *new* Transfer. */
export class Transfer {
  constructor({ id, idempotencyKey, fromWalletId, toWalletId, amount, status, failureReason, createdAt, updatedAt }) {
    this.id = id;
    this.idempotencyKey = idempotencyKey ?? null;
    this.fromWalletId = fromWalletId;
    this.toWalletId = toWalletId;
    this.amount = amount;
    this.status = status;
    this.failureReason = failureReason ?? null;
    this.createdAt = createdAt;
    this.updatedAt = updatedAt;
    Object.freeze(this);
  }

  static newPending({ fromWalletId, toWalletId, amount, idempotencyKey, now }) {
    return new Transfer({
      id: randomUUID(), idempotencyKey, fromWalletId, toWalletId, amount,
      status: TransferStatus.PENDING, failureReason: null, createdAt: now, updatedAt: now,
    });
  }

  canTransitionTo(newStatus) {
    return ALLOWED_TRANSITIONS[this.status].includes(newStatus);
  }

  transitionTo(newStatus, now, failureReason = null) {
    if (!this.canTransitionTo(newStatus)) {
      throw new InvalidStateTransition(`transfer ${this.id}: ${this.status} -> ${newStatus} is not allowed`);
    }
    if (newStatus === TransferStatus.FAILED && !failureReason) {
      throw new InvalidStateTransition('a FAILED transfer requires a failure reason');
    }
    if (newStatus !== TransferStatus.FAILED && failureReason) {
      throw new InvalidStateTransition('only a FAILED transfer may carry a failure reason');
    }
    return new Transfer({ ...this, status: newStatus, failureReason, updatedAt: now });
  }
}

export function validateWalletId(value, field) {
  if (typeof value !== 'string' || value.trim() === '') {
    throw new ValidationError(`${field} must be a non-empty string`);
  }
  if (value.length > MAX_ID_LENGTH) {
    throw new ValidationError(`${field} must be at most ${MAX_ID_LENGTH} characters`);
  }
  return value;
}

export function validateAmount(value, field = 'amount') {
  // typeof true === 'boolean' and '5' is a string, so only real numbers get through;
  // isSafeInteger rejects 1.5, NaN, Infinity and values beyond 2^53.
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) {
    throw new ValidationError(`${field} must be an integer (minor units)`);
  }
  if (value <= 0) throw new ValidationError(`${field} must be greater than zero`);
  if (value > MAX_AMOUNT) throw new ValidationError(`${field} must be at most ${MAX_AMOUNT}`);
  return value;
}

export function validateTransferRequest({ fromWalletId, toWalletId, amount, idempotencyKey }) {
  validateWalletId(fromWalletId, 'fromWalletId');
  validateWalletId(toWalletId, 'toWalletId');
  if (fromWalletId === toWalletId) throw new ValidationError('fromWalletId and toWalletId must differ');
  validateAmount(amount);
  if (idempotencyKey !== undefined && idempotencyKey !== null) {
    if (typeof idempotencyKey !== 'string' || idempotencyKey.trim() === '') {
      throw new ValidationError('idempotencyKey must be a non-empty string');
    }
    if (idempotencyKey.length > MAX_KEY_LENGTH) {
      throw new ValidationError(`idempotencyKey must be at most ${MAX_KEY_LENGTH} characters`);
    }
  }
}

/** Stable hash of the business payload; detects key re-use with different data. */
export function requestFingerprint(fromWalletId, toWalletId, amount) {
  return createHash('sha256').update(JSON.stringify([fromWalletId, toWalletId, amount])).digest('hex');
}
