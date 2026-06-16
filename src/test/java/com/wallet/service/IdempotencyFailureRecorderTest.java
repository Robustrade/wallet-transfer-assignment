package com.wallet.service;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.controller.dto.CreateTransferRequest;
import com.wallet.domain.Transfer;
import com.wallet.domain.TransferStatus;
import com.wallet.metrics.TransferMetrics;
import com.wallet.repository.IdempotencyRecordRepository;
import com.wallet.repository.TransferRepository;
import io.micrometer.core.instrument.simple.SimpleMeterRegistry;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;
import org.mockito.Mockito;

import java.time.Instant;
import java.util.UUID;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyInt;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoInteractions;
import static org.mockito.Mockito.when;

/**
 * Unit tests for {@link IdempotencyFailureRecorder}. The integration suite
 * exercises the happy path end-to-end; this suite locks down the contract that
 * is most easily missed in integration: the orphan-transfer-prevention path
 * when a concurrent retry has already won the idempotency claim.
 */
class IdempotencyFailureRecorderTest {

    private static final String CLIENT_ID = "test-client";
    private static final String IDEMPOTENCY_KEY = "key-1";
    private static final String REQUEST_HASH = "deadbeef";

    private static Transfer dbAssignedTransfer(Transfer in) {
        Instant now = Instant.now();
        return new Transfer(
                in.getId(),
                in.getFromWalletId(),
                in.getToWalletId(),
                in.getAmount(),
                in.getStatus(),
                in.getErrorMessage(),
                now,
                now
        );
    }

    @Test
    @DisplayName("Insufficient balance — happy path: Transfer inserted first, then idempotency claim carries transfer_id")
    void shouldInsertTransferThenIdempotencyOnInsufficientBalance() {
        IdempotencyRecordRepository idempotencyRepo = Mockito.mock(IdempotencyRecordRepository.class);
        TransferRepository transferRepo = Mockito.mock(TransferRepository.class);
        ObjectMapper objectMapper = new ObjectMapper();

        when(idempotencyRepo.insertFailedRecord(
                anyString(), anyString(), anyString(), any(), anyInt(), anyString())).thenReturn(1);
        when(transferRepo.insert(any(Transfer.class)))
                .thenAnswer(inv -> dbAssignedTransfer(inv.getArgument(0)));

        IdempotencyFailureRecorder recorder = new IdempotencyFailureRecorder(
                idempotencyRepo, transferRepo, objectMapper, new TransferMetrics(new SimpleMeterRegistry()));

        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 500L);
        recorder.recordInsufficientBalanceFailure(CLIENT_ID, IDEMPOTENCY_KEY, REQUEST_HASH, request, 100L);

        // 1. Audit Transfer is inserted first (no FK back to idempotency).
        ArgumentCaptor<Transfer> transferCap = ArgumentCaptor.forClass(Transfer.class);
        verify(transferRepo, times(1)).insert(transferCap.capture());
        Transfer inserted = transferCap.getValue();
        org.assertj.core.api.Assertions.assertThat(inserted.getStatus()).isEqualTo(TransferStatus.FAILED);
        org.assertj.core.api.Assertions.assertThat(inserted.getFromWalletId()).isEqualTo("wallet_1");
        org.assertj.core.api.Assertions.assertThat(inserted.getToWalletId()).isEqualTo("wallet_2");
        org.assertj.core.api.Assertions.assertThat(inserted.getAmount()).isEqualTo(500L);

        // 2. Idempotency claim follows, already carrying transfer_id — no separate linkTransfer call.
        verify(idempotencyRepo, times(1)).insertFailedRecord(
                eq(CLIENT_ID), eq(IDEMPOTENCY_KEY), eq(REQUEST_HASH),
                eq(inserted.getId()), eq(422), anyString());
    }

    @Test
    @DisplayName("Insufficient balance — concurrent retry won: Transfer is still written so audit is never lost")
    void shouldKeepAuditTransferWhenConcurrentRetryWonClaim() {
        IdempotencyRecordRepository idempotencyRepo = Mockito.mock(IdempotencyRecordRepository.class);
        TransferRepository transferRepo = Mockito.mock(TransferRepository.class);
        ObjectMapper objectMapper = new ObjectMapper();

        // Claim returns 0 — a concurrent retry already wrote the idempotency row.
        when(idempotencyRepo.insertFailedRecord(
                anyString(), anyString(), anyString(), any(), anyInt(), anyString())).thenReturn(0);
        when(transferRepo.insert(any(Transfer.class)))
                .thenAnswer(inv -> dbAssignedTransfer(inv.getArgument(0)));

        IdempotencyFailureRecorder recorder = new IdempotencyFailureRecorder(
                idempotencyRepo, transferRepo, objectMapper, new TransferMetrics(new SimpleMeterRegistry()));

        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 500L);
        recorder.recordInsufficientBalanceFailure(CLIENT_ID, IDEMPOTENCY_KEY, REQUEST_HASH, request, 100L);

        // Audit Transfer is written even when the idempotency claim is lost — losing
        // audit data is worse than carrying an unreferenced FAILED row.
        verify(transferRepo, times(1)).insert(any(Transfer.class));
        verify(idempotencyRepo, times(1)).insertFailedRecord(
                anyString(), anyString(), anyString(), any(UUID.class), anyInt(), anyString());
    }

    @Test
    @DisplayName("Wallet not found — writes idempotency-only, never a Transfer row (FK would fail)")
    void shouldWriteOnlyIdempotencyOnWalletNotFound() {
        IdempotencyRecordRepository idempotencyRepo = Mockito.mock(IdempotencyRecordRepository.class);
        TransferRepository transferRepo = Mockito.mock(TransferRepository.class);
        ObjectMapper objectMapper = new ObjectMapper();

        when(idempotencyRepo.insertFailedRecord(
                anyString(), anyString(), anyString(), any(), anyInt(), anyString())).thenReturn(1);

        IdempotencyFailureRecorder recorder = new IdempotencyFailureRecorder(
                idempotencyRepo, transferRepo, objectMapper, new TransferMetrics(new SimpleMeterRegistry()));

        recorder.recordWalletNotFoundFailure(CLIENT_ID, IDEMPOTENCY_KEY, REQUEST_HASH,
                "Wallet not found: wallet_unknown");

        // Idempotency record carries 404 status and a null transfer_id — the
        // wallet FK on transfers makes it impossible to insert an audit row.
        verify(idempotencyRepo, times(1)).insertFailedRecord(
                eq(CLIENT_ID), eq(IDEMPOTENCY_KEY), eq(REQUEST_HASH),
                eq(null), eq(404), anyString());
        verifyNoInteractions(transferRepo);
    }

    @Test
    @DisplayName("Wallet not found — concurrent retry already won: still no Transfer row written")
    void shouldNoOpOnWalletNotFoundWhenClaimAlreadyExists() {
        IdempotencyRecordRepository idempotencyRepo = Mockito.mock(IdempotencyRecordRepository.class);
        TransferRepository transferRepo = Mockito.mock(TransferRepository.class);
        ObjectMapper objectMapper = new ObjectMapper();

        when(idempotencyRepo.insertFailedRecord(
                anyString(), anyString(), anyString(), any(), anyInt(), anyString())).thenReturn(0);

        IdempotencyFailureRecorder recorder = new IdempotencyFailureRecorder(
                idempotencyRepo, transferRepo, objectMapper, new TransferMetrics(new SimpleMeterRegistry()));

        recorder.recordWalletNotFoundFailure(CLIENT_ID, IDEMPOTENCY_KEY, REQUEST_HASH,
                "Wallet not found: wallet_unknown");

        verifyNoInteractions(transferRepo);
    }
}
