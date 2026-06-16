package com.wallet.service;

import com.wallet.controller.dto.CreateTransferRequest;
import com.wallet.exception.IdempotencyConflictException;
import com.wallet.exception.TransientIdempotencyConflictException;
import com.wallet.metrics.TransferMetrics;
import com.wallet.repository.ClientRepository;
import com.wallet.repository.IdempotencyRecordRepository;
import com.wallet.util.Hashing;
import io.micrometer.core.instrument.simple.SimpleMeterRegistry;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.mockito.Mockito;

import java.util.Optional;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

/**
 * Unit tests for TransferService utility methods and the bounded-retry path
 * for rolled-back idempotency records.
 */
class TransferServiceTest {

    @Test
    @DisplayName("Should produce consistent SHA-256 hash for same input")
    void shouldProduceConsistentHash() {
        String input = "wallet_1|wallet_2|100";

        String hash1 = Hashing.sha256Hex(input);
        String hash2 = Hashing.sha256Hex(input);

        assertThat(hash1).isEqualTo(hash2);
        assertThat(hash1).hasSize(64); // SHA-256 = 64 hex chars
    }

    @Test
    @DisplayName("Should produce different hashes for different inputs")
    void shouldProduceDifferentHashes() {
        String input1 = "wallet_1|wallet_2|100";
        String input2 = "wallet_1|wallet_2|200";

        String hash1 = Hashing.sha256Hex(input1);
        String hash2 = Hashing.sha256Hex(input2);

        assertThat(hash1).isNotEqualTo(hash2);
    }

    @Test
    @DisplayName("Should produce different hashes when wallets are swapped")
    void shouldProduceDifferentHashesForSwappedWallets() {
        String input1 = "wallet_1|wallet_2|100";
        String input2 = "wallet_2|wallet_1|100";

        String hash1 = Hashing.sha256Hex(input1);
        String hash2 = Hashing.sha256Hex(input2);

        assertThat(hash1).isNotEqualTo(hash2);
    }

    /**
     * The retry path for "conflict observed but record not found" must be bounded.
     * If the underlying {@link TransferExecutionService} repeatedly raises a
     * {@link IdempotencyConflictException} while the row is absent on read (a
     * persistent rolled-back race), the service must give up after a finite
     * number of attempts and surface the <em>transient</em> conflict — never
     * loop indefinitely, and never present the failure as a 409 body-mismatch
     * (that's a different kind of error and would mislead an oncall).
     */
    @Test
    @DisplayName("Should give up after max attempts when conflict + missing record persists")
    void shouldBoundConflictRetries() {
        IdempotencyRecordRepository idempotencyRepo = Mockito.mock(IdempotencyRecordRepository.class);
        TransferExecutionService executionService = Mockito.mock(TransferExecutionService.class);
        IdempotencyFailureRecorder recorder = Mockito.mock(IdempotencyFailureRecorder.class);
        ClientRepository clientRepository = Mockito.mock(ClientRepository.class);
        TransferMetrics metrics = new TransferMetrics(new SimpleMeterRegistry());

        when(clientRepository.existsById(anyString())).thenReturn(true);

        // Always conflict, never recover — the test asserts the loop is bounded.
        when(executionService.tryExecuteTransfer(anyString(), anyString(), any(), anyString()))
                .thenThrow(new IdempotencyConflictException("k"));

        // findById always returns empty → caller should retry rather than replay.
        when(idempotencyRepo.findById(anyString(), anyString())).thenReturn(Optional.empty());

        // Pass the retry knobs explicitly. base/jitter set to 0 so the test
        // doesn't pay for real Thread.sleep — the bound itself is what we
        // want to assert. retry-after is irrelevant for this test path.
        int maxAttempts = TransferService.DEFAULT_MAX_EXECUTION_ATTEMPTS;
        TransferService service = new TransferService(
                idempotencyRepo, executionService, recorder, clientRepository, metrics,
                maxAttempts, 0L, 0L, TransferService.DEFAULT_TRANSIENT_CONFLICT_RETRY_AFTER_SECONDS);

        CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 100L);

        // Expect the transient variant, NOT the regular 409 — the request body
        // never changed, the system simply thrashed past its retry budget.
        assertThatThrownBy(() -> service.executeTransfer("test-client", "k", request))
                .isInstanceOf(TransientIdempotencyConflictException.class);

        // Read the bound from the value passed in rather than hard-coding "3"
        // so any future tuning of the default only needs to change one site.
        verify(executionService, times(maxAttempts))
                .tryExecuteTransfer(anyString(), anyString(), any(), anyString());
        verify(idempotencyRepo, times(maxAttempts))
                .findById(anyString(), anyString());
    }
}
