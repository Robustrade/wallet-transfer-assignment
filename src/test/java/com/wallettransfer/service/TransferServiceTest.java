package com.wallettransfer.service;

import com.wallettransfer.api.TransferRequest;
import com.wallettransfer.api.TransferResponse;
import com.wallettransfer.api.WalletResponse;
import com.wallettransfer.domain.LedgerEntryType;
import com.wallettransfer.domain.TransferStatus;
import com.wallettransfer.repository.LedgerRepository;
import com.wallettransfer.repository.TransferRepository;
import com.wallettransfer.repository.WalletRepository;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import java.math.BigDecimal;
import java.time.OffsetDateTime;
import java.util.UUID;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

@ExtendWith(MockitoExtension.class)
class TransferServiceTest {
    @Mock WalletRepository wallets;
    @Mock TransferRepository transfers;
    @Mock LedgerRepository ledger;
    @InjectMocks TransferService service;

    @Test
    void transfersFundsAndWritesBalancedLedgerEntries() {
        TransferRequest request = request("key-1", "wallet-a", "wallet-b", "25.00");
        TransferResponse response = response(TransferStatus.PROCESSED, request);
        when(transfers.claimIdempotency(eq("key-1"), any(), eq("wallet-a"), eq("wallet-b"), eq(new BigDecimal("25.00"))))
                .thenReturn(true);
        when(transfers.findByIdempotencyKeyWithLock("key-1")).thenReturn(response(TransferStatus.PENDING, request));
        when(wallets.findByIdForUpdate("wallet-a")).thenReturn(new WalletResponse("wallet-a", new BigDecimal("100.00")));
        when(wallets.findByIdForUpdate("wallet-b")).thenReturn(new WalletResponse("wallet-b", new BigDecimal("10.00")));
        when(transfers.findById(any(UUID.class))).thenReturn(response);

        assertEquals(response, service.create(request));
        verify(wallets).updateBalance("wallet-a", new BigDecimal("75.00"));
        verify(wallets).updateBalance("wallet-b", new BigDecimal("35.00"));
        verify(ledger).add(any(UUID.class), eq("wallet-a"), eq(LedgerEntryType.DEBIT), eq(new BigDecimal("25.00")));
        verify(ledger).add(any(UUID.class), eq("wallet-b"), eq(LedgerEntryType.CREDIT), eq(new BigDecimal("25.00")));
    }

    @Test
    void replaysAnExistingIdempotentRequestWithoutSideEffects() {
        TransferRequest request = request("key-2", "wallet-a", "wallet-b", "10");
        TransferResponse response = response(TransferStatus.PROCESSED, request);
        when(transfers.claimIdempotency(eq("key-2"), any(), eq("wallet-a"), eq("wallet-b"), eq(new BigDecimal("10"))))
                .thenReturn(false);
        when(transfers.findByIdempotencyKeyWithLock("key-2")).thenReturn(response);

        assertEquals(response, service.create(request));
        verify(wallets, never()).findByIdForUpdate(any());
        verify(ledger, never()).add(any(), any(), any(), any());
    }

    @Test
    void rejectsReusingAnIdempotencyKeyWithDifferentTransferData() {
        TransferRequest request = request("key-2-reused", "wallet-a", "wallet-b", "10");
        TransferResponse existing = response(TransferStatus.PROCESSED,
                request("key-2-reused", "wallet-a", "wallet-b", "20"));
        when(transfers.claimIdempotency(eq("key-2-reused"), any(), eq("wallet-a"), eq("wallet-b"), eq(new BigDecimal("10"))))
                .thenReturn(false);
        when(transfers.findByIdempotencyKeyWithLock("key-2-reused")).thenReturn(existing);

        assertThrows(InvalidTransferException.class, () -> service.create(request));
        verify(wallets, never()).findByIdForUpdate(any());
        verify(ledger, never()).add(any(), any(), any(), any());
    }

    @Test
    void recordsFailureAndDoesNotWriteLedgerWhenFundsAreInsufficient() {
        TransferRequest request = request("key-3", "wallet-a", "wallet-b", "101");
        when(transfers.claimIdempotency(eq("key-3"), any(), eq("wallet-a"), eq("wallet-b"), eq(new BigDecimal("101"))))
                .thenReturn(true);
        when(transfers.findByIdempotencyKeyWithLock("key-3")).thenReturn(response(TransferStatus.PENDING, request));
        when(wallets.findByIdForUpdate("wallet-a")).thenReturn(new WalletResponse("wallet-a", new BigDecimal("100")));
        when(wallets.findByIdForUpdate("wallet-b")).thenReturn(new WalletResponse("wallet-b", BigDecimal.ZERO));

        assertThrows(InsufficientFundsException.class, () -> service.create(request));
        verify(transfers).markFailed(any(UUID.class), eq("Insufficient funds in wallet: wallet-a"));
        verify(ledger, never()).add(any(), any(), any(), any());
    }

    private TransferRequest request(String key, String from, String to, String amount) {
        return new TransferRequest(key, from, to, new BigDecimal(amount));
    }

    private TransferResponse response(TransferStatus status, TransferRequest request) {
        return new TransferResponse(UUID.randomUUID(), request.idempotencyKey(), request.fromWalletId(),
                request.toWalletId(), request.amount(), status, OffsetDateTime.now());
    }
}