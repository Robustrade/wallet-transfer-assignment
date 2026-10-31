// Domain errors. They carry no HTTP knowledge; the handler layer maps `code` to a status.

export class DomainError extends Error {
  constructor(message) {
    super(message);
    this.name = this.constructor.name;
  }
}

export class ValidationError extends DomainError { code = 'VALIDATION_ERROR'; }
export class WalletNotFound extends DomainError { code = 'WALLET_NOT_FOUND'; }
export class WalletAlreadyExists extends DomainError { code = 'WALLET_ALREADY_EXISTS'; }
export class TransferNotFound extends DomainError { code = 'TRANSFER_NOT_FOUND'; }
/** Same idempotency key re-used with a different request payload. */
export class IdempotencyKeyConflict extends DomainError { code = 'IDEMPOTENCY_KEY_CONFLICT'; }
export class InvalidStateTransition extends DomainError { code = 'INVALID_STATE_TRANSITION'; }
