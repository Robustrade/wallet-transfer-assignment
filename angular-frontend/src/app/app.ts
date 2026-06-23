import { Component, OnInit, signal, inject } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';

@Component({
  selector: 'app-root',
  standalone: true,
  imports: [CommonModule, FormsModule],
  templateUrl: './app.html',
  styleUrl: './app.css'
})
export class App implements OnInit {
  private readonly http = inject(HttpClient);

  // Signals to hold state reactive variables
  wallets = signal<any[]>([]);
  transfers = signal<any[]>([]);

  // Form Fields
  fromWalletId = '';
  toWalletId = '';
  amount: number | null = null;
  idempotencyKey = '';

  // Status/Error Messages
  successMessage = '';
  errorMessage = '';
  isSubmitting = false;

  ngOnInit() {
    this.refreshData();
    this.generateIdempotencyKey();
  }

  // Generates a random idempotency key for testing exactly-once delivery
  generateIdempotencyKey() {
    this.idempotencyKey = 'key-' + Math.random().toString(36).substring(2, 9);
  }

  // Loads wallets and transfers from the Spring Boot API
  refreshData() {
    // 1. Fetch Wallets and Balances
    this.http.get<any[]>('http://localhost:8080/transfers/wallets').subscribe({
      next: (data) => {
        this.wallets.set(data);
        this.errorMessage = ''; // clear error once connected
      },
      error: (err) => {
        console.error('Failed to connect to backend', err);
        this.errorMessage = 'Cannot connect to backend server. Make sure Spring Boot is running on port 8080!';
      }
    });

    // 2. Fetch Historical Transfers
    this.http.get<any[]>('http://localhost:8080/transfers').subscribe({
      next: (data) => this.transfers.set(data),
      error: (err) => console.error('Failed to load transfers', err)
    });
  }

  // Submits the transfer request
  submitTransfer() {
    if (!this.fromWalletId || !this.toWalletId || !this.amount || !this.idempotencyKey) {
      this.errorMessage = 'Please fill out all fields.';
      return;
    }

    if (this.fromWalletId === this.toWalletId) {
      this.errorMessage = 'Source and destination wallets must be different.';
      return;
    }

    if (this.amount <= 0) {
      this.errorMessage = 'Amount must be greater than zero.';
      return;
    }

    this.errorMessage = '';
    this.successMessage = '';
    this.isSubmitting = true;

    const payload = {
      idempotencyKey: this.idempotencyKey,
      fromWalletId: this.fromWalletId,
      toWalletId: this.toWalletId,
      amount: this.amount
    };

    // Call POST /transfers
    this.http.post<any>('http://localhost:8080/transfers', payload).subscribe({
      next: (response) => {
        this.isSubmitting = false;
        if (response.status === 'PROCESSED') {
          this.successMessage = `Transfer of $${payload.amount} successful! ID: ${response.transferId}`;
          this.amount = null; // Clear amount input
          this.generateIdempotencyKey(); // Auto-generate new key for next test
          this.refreshData(); // Refresh list and wallet cards
        } else {
          this.errorMessage = `Transfer failed: ${response.errorMessage || 'Unknown error'}`;
        }
      },
      error: (errorResponse) => {
        this.isSubmitting = false;
        const err = errorResponse.error;
        this.errorMessage = err?.errorMessage || err?.message || 'Transfer failed. Check details or console.';
        this.refreshData(); // Refresh to see if balances were updated
      }
    });
  }
}
