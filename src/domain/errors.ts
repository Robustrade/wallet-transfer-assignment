export class TransferError extends Error {
  constructor(
    message: string,
    public readonly code: string,
    public readonly statusCode: number = 422,
  ) {
    super(message);
    this.name = 'TransferError';
  }
}

export class WalletNotFoundError extends TransferError {
  constructor(walletIds: string[]) {
    super(`Wallet(s) not found: ${walletIds.join(', ')}`, 'WALLET_NOT_FOUND', 404);
  }
}

export class InsufficientBalanceError extends TransferError {
  constructor() {
    super('Insufficient balance in source wallet', 'INSUFFICIENT_BALANCE', 422);
  }
}

export class ValidationError extends Error {
  constructor(
    message: string,
    public readonly details: unknown,
  ) {
    super(message);
    this.name = 'ValidationError';
  }
}
