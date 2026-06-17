import { DatePipe, DecimalPipe } from '@angular/common';
import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { WalletApiService } from '../services/wallet-api.service';
import { LedgerEntry, Transfer, Wallet, WalletHistory } from '../models/wallet.models';
import { ChartPanel } from './chart-panel';

type AppView = 'overview' | 'transfer' | 'ledger' | 'history' | 'analytics';

interface NavItem {
  id: AppView;
  label: string;
  icon: string;
  description: string;
}

@Component({
  selector: 'app-dashboard',
  imports: [ReactiveFormsModule, DecimalPipe, DatePipe, ChartPanel],
  templateUrl: './dashboard.html',
  styleUrl: './dashboard.css',
})
export class Dashboard implements OnInit {
  private readonly api = inject(WalletApiService);
  private readonly fb = inject(FormBuilder);

  readonly navItems: NavItem[] = [
    { id: 'overview', label: 'Overview', icon: '◫', description: 'Wallet summary and KPIs' },
    { id: 'transfer', label: 'Transfer', icon: '⇄', description: 'Send money between wallets' },
    { id: 'ledger', label: 'Ledger', icon: '☰', description: 'Double-entry ledger entries' },
    { id: 'history', label: 'History', icon: '↺', description: 'Transfer activity log' },
    { id: 'analytics', label: 'Analytics', icon: '▤', description: 'Volume and transaction trends' },
  ];

  readonly wallets = signal<Wallet[]>([]);
  readonly selectedWalletId = signal<string | null>(null);
  readonly history = signal<WalletHistory | null>(null);
  readonly activeView = signal<AppView>('overview');
  readonly loading = signal(false);
  readonly message = signal<string | null>(null);
  readonly error = signal<string | null>(null);
  readonly showAddWallet = signal(false);

  readonly selectedWallet = computed(() => {
    const id = this.selectedWalletId();
    return this.wallets().find((wallet) => wallet.id === id) ?? null;
  });

  readonly activeNav = computed(() =>
    this.navItems.find((item) => item.id === this.activeView()) ?? this.navItems[0],
  );

  readonly transferForm = this.fb.nonNullable.group({
    idempotencyKey: ['', Validators.required],
    fromWalletId: ['', Validators.required],
    toWalletId: ['', Validators.required],
    amount: [100, [Validators.required, Validators.min(1)]],
  });

  readonly walletForm = this.fb.nonNullable.group({
    id: ['', [Validators.required, Validators.pattern(/^[a-zA-Z0-9_-]+$/)]],
    initialBalance: [0, [Validators.required, Validators.min(0)]],
  });

  ngOnInit(): void {
    this.newIdempotencyKey();
    this.refreshWallets();
  }

  setView(view: AppView): void {
    this.activeView.set(view);
    this.message.set(null);
    this.error.set(null);
  }

  refreshWallets(selectWalletId?: string): void {
    this.loading.set(true);
    this.error.set(null);

    this.api.getWallets().subscribe({
      next: (wallets) => {
        this.wallets.set(wallets);
        const current = selectWalletId ?? this.selectedWalletId();
        const fallback = wallets[0]?.id ?? null;
        const nextId = wallets.some((wallet) => wallet.id === current) ? current : fallback;
        if (nextId) {
          this.selectWallet(nextId);
        } else {
          this.selectedWalletId.set(null);
          this.history.set(null);
          this.loading.set(false);
        }
      },
      error: () => {
        this.error.set('Failed to load wallets');
        this.loading.set(false);
      },
    });
  }

  selectWallet(walletId: string, reloadHistory = true): void {
    this.selectedWalletId.set(walletId);
    this.transferForm.patchValue({
      fromWalletId: walletId,
      toWalletId: this.pickOtherWallet(walletId),
    });
    if (reloadHistory) {
      this.loadHistory(walletId);
    }
  }

  loadHistory(walletId: string): void {
    this.loading.set(true);
    this.api.getWalletHistory(walletId).subscribe({
      next: (history) => {
        this.history.set(history);
        this.loading.set(false);
      },
      error: () => {
        this.error.set('Failed to load wallet history');
        this.loading.set(false);
      },
    });
  }

  createWallet(): void {
    if (this.walletForm.invalid) {
      this.walletForm.markAllAsTouched();
      return;
    }

    this.loading.set(true);
    this.error.set(null);
    this.message.set(null);

    const payload = this.walletForm.getRawValue();
    this.api.createWallet(payload).subscribe({
      next: (wallet) => {
        this.message.set(`Wallet ${wallet.id} created`);
        this.walletForm.reset({ id: '', initialBalance: 0 });
        this.showAddWallet.set(false);
        this.refreshWallets(wallet.id);
      },
      error: (err) => {
        this.loading.set(false);
        const body = err.error as { message?: string } | undefined;
        this.error.set(body?.message ?? 'Failed to create wallet');
      },
    });
  }

  submitTransfer(): void {
    if (this.transferForm.invalid) {
      this.transferForm.markAllAsTouched();
      return;
    }

    this.loading.set(true);
    this.error.set(null);
    this.message.set(null);

    this.api.createTransfer(this.transferForm.getRawValue()).subscribe({
      next: (transfer) => {
        const label = transfer.duplicate ? 'Duplicate request returned cached result' : 'Transfer completed';
        this.message.set(`${label}: ${transfer.status}`);
        this.refreshWallets(this.selectedWalletId() ?? transfer.fromWalletId);
      },
      error: (err) => {
        this.loading.set(false);
        const body = err.error as { message?: string } | undefined;
        this.error.set(body?.message ?? 'Transfer request failed');
        const walletId = this.selectedWalletId();
        if (walletId) {
          this.loadHistory(walletId);
        }
      },
    });
  }

  resendSameKey(): void {
    this.submitTransfer();
  }

  newIdempotencyKey(): void {
    this.transferForm.patchValue({ idempotencyKey: crypto.randomUUID() });
  }

  toggleAddWallet(): void {
    this.showAddWallet.update((value) => !value);
  }

  transferDirection(transfer: Transfer, walletId: string): string {
    return transfer.fromWalletId === walletId ? 'OUT' : 'IN';
  }

  ledgerSignedAmount(entry: LedgerEntry): number {
    return entry.entryType === 'CREDIT' ? entry.amount : -entry.amount;
  }

  private pickOtherWallet(walletId: string): string {
    return this.wallets().find((wallet) => wallet.id !== walletId)?.id ?? walletId;
  }
}
