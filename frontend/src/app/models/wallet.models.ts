export interface Wallet {
  id: string;
  balance: number;
}

export interface CreateWalletRequest {
  id: string;
  initialBalance: number;
}

export type TransferStatus = 'PENDING' | 'PROCESSED' | 'FAILED';

export interface Transfer {
  id: string;
  idempotencyKey: string | null;
  fromWalletId: string;
  toWalletId: string;
  amount: number;
  status: TransferStatus;
  failureReason: string | null;
  duplicate: boolean;
  createdAt: string;
}

export interface LedgerEntry {
  id: number;
  transferId: string;
  walletId: string;
  entryType: 'DEBIT' | 'CREDIT';
  amount: number;
  createdAt: string;
}

export interface TransactionDayStat {
  date: string;
  count: number;
  volume: number;
}

export interface WalletHistory {
  walletId: string;
  balance: number;
  ledgerBalance: number;
  totalDebits: number;
  totalCredits: number;
  processedTransfers: number;
  failedTransfers: number;
  transfers: Transfer[];
  ledgerEntries: LedgerEntry[];
  dailyStats: TransactionDayStat[];
}

export interface CreateTransferRequest {
  idempotencyKey: string;
  fromWalletId: string;
  toWalletId: string;
  amount: number;
}

export interface ErrorResponse {
  message: string;
}
