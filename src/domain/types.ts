export type TransferStatus = 'PENDING' | 'PROCESSED' | 'FAILED';
export type LedgerEntryType = 'DEBIT' | 'CREDIT';

export interface Wallet {
  id: string;
  balance: string;
  createdAt: Date;
}

export interface Transfer {
  id: string;
  idempotencyKey: string;
  fromWalletId: string;
  toWalletId: string;
  amount: string;
  status: TransferStatus;
  failureReason: string | null;
  createdAt: Date;
  updatedAt: Date;
}

export interface LedgerEntry {
  id: string;
  walletId: string;
  transferId: string;
  type: LedgerEntryType;
  amount: string;
  createdAt: Date;
}

export interface CreateTransferInput {
  idempotencyKey: string;
  fromWalletId: string;
  toWalletId: string;
  amount: number;
}

export interface TransferResult {
  transfer: Transfer;
  ledgerEntries: LedgerEntry[];
  duplicate: boolean;
}
