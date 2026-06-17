import { Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import {
  CreateTransferRequest,
  CreateWalletRequest,
  LedgerEntry,
  Transfer,
  Wallet,
  WalletHistory,
} from '../models/wallet.models';

@Injectable({ providedIn: 'root' })
export class WalletApiService {
  private readonly baseUrl = '/api';

  constructor(private readonly http: HttpClient) {}

  getWallets(): Observable<Wallet[]> {
    return this.http.get<Wallet[]>(`${this.baseUrl}/wallets`);
  }

  createWallet(request: CreateWalletRequest): Observable<Wallet> {
    return this.http.post<Wallet>(`${this.baseUrl}/wallets`, request);
  }

  getWalletHistory(walletId: string): Observable<WalletHistory> {
    return this.http.get<WalletHistory>(`${this.baseUrl}/wallets/${walletId}/history`);
  }

  getTransfers(): Observable<Transfer[]> {
    return this.http.get<Transfer[]>(`${this.baseUrl}/transfers`);
  }

  getLedger(transferId: string): Observable<LedgerEntry[]> {
    return this.http.get<LedgerEntry[]>(`${this.baseUrl}/transfers/${transferId}/ledger`);
  }

  createTransfer(request: CreateTransferRequest): Observable<Transfer> {
    return this.http.post<Transfer>(`${this.baseUrl}/transfers`, request);
  }
}
