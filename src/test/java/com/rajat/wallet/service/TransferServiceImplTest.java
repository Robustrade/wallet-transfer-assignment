package com.rajat.wallet.service;

import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.rajat.wallet.dto.CreateTransferRequest;
import com.rajat.wallet.repository.IdempotencyRecordRepository;
import java.math.BigDecimal;
import java.sql.SQLException;
import java.util.Optional;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.dao.DataIntegrityViolationException;

/**
 * Verifies that the orchestrator only treats the idempotency-key unique violation as a replayable
 * race; any other integrity violation is rethrown rather than misread as a duplicate.
 */
class TransferServiceImplTest {

  private final TransferProcessor processor = mock(TransferProcessor.class);
  private final IdempotencyRecordRepository idempotencyRepository =
      mock(IdempotencyRecordRepository.class);
  private final TransferServiceImpl service =
      new TransferServiceImpl(processor, idempotencyRepository, new ObjectMapper());

  private final CreateTransferRequest request =
      new CreateTransferRequest(
          "key-1", UUID.randomUUID(), UUID.randomUUID(), new BigDecimal("10.00"));

  @Test
  void rethrowsNonIdempotencyIntegrityViolationWithoutAttemptingReplay() {
    when(idempotencyRepository.findByIdempotencyKey("key-1")).thenReturn(Optional.empty());
    when(processor.process(any(), any()))
        .thenThrow(
            new DataIntegrityViolationException(
                "could not execute statement",
                new SQLException("violates foreign key constraint \"fk_ledger_transfer\"")));

    assertThatThrownBy(() -> service.createTransfer(request))
        .isInstanceOf(DataIntegrityViolationException.class);

    // Only the initial fast-path lookup ran — it never entered the winner-replay path.
    verify(idempotencyRepository, times(1)).findByIdempotencyKey("key-1");
  }

  @Test
  void entersReplayPathForIdempotencyKeyViolation() {
    when(idempotencyRepository.findByIdempotencyKey("key-1")).thenReturn(Optional.empty());
    when(processor.process(any(), any()))
        .thenThrow(
            new DataIntegrityViolationException(
                "could not execute statement",
                new SQLException(
                    "duplicate key value violates unique constraint"
                        + " \"idempotency_records_key_unique\"")));

    // The winner lookup also returns empty here, so it rethrows — but it DID attempt the replay,
    // i.e. it queried for the winner a second time.
    assertThatThrownBy(() -> service.createTransfer(request))
        .isInstanceOf(DataIntegrityViolationException.class);

    verify(idempotencyRepository, times(2)).findByIdempotencyKey("key-1");
  }
}
