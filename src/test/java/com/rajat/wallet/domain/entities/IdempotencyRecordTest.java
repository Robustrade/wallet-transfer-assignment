package com.rajat.wallet.domain.entities;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.rajat.wallet.domain.enums.IdempotencyStatus;
import java.util.UUID;
import org.junit.jupiter.api.Test;

/**
 * Unit tests for the {@link IdempotencyRecord} lifecycle: it starts IN_PROGRESS, captures the
 * response on completion, and — crucially for exactly-once replay — completion is one-way so the
 * cached response can never be overwritten.
 */
class IdempotencyRecordTest {

  @Test
  void inProgressStartsInProgressWithNoCachedResponse() {
    IdempotencyRecord record = IdempotencyRecord.inProgress("key-1", "hash-1");

    assertThat(record.getStatus()).isEqualTo(IdempotencyStatus.IN_PROGRESS);
    assertThat(record.getIdempotencyKey()).isEqualTo("key-1");
    assertThat(record.getRequestHash()).isEqualTo("hash-1");
    assertThat(record.getTargetId()).isNull();
    assertThat(record.getResponseStatus()).isNull();
    assertThat(record.getResponseBody()).isNull();
  }

  @Test
  void completeCapturesTheResponseAndMarksCompleted() {
    IdempotencyRecord record = IdempotencyRecord.inProgress("key-1", "hash-1");
    UUID target = UUID.randomUUID();

    record.complete(target, 201, "{\"transferId\":\"abc\"}");

    assertThat(record.getStatus()).isEqualTo(IdempotencyStatus.COMPLETED);
    assertThat(record.getTargetId()).isEqualTo(target);
    assertThat(record.getResponseStatus()).isEqualTo(201);
    assertThat(record.getResponseBody()).isEqualTo("{\"transferId\":\"abc\"}");
  }

  @Test
  void completeIsOneWayAndRejectsASecondCall() {
    IdempotencyRecord record = IdempotencyRecord.inProgress("key-1", "hash-1");
    record.complete(UUID.randomUUID(), 201, "{}");

    assertThatThrownBy(() -> record.complete(UUID.randomUUID(), 422, "{\"overwritten\":true}"))
        .isInstanceOf(IllegalStateException.class);
  }
}
