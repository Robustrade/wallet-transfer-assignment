package com.rajat.wallet.exception.handler;

import static org.assertj.core.api.Assertions.assertThat;

import com.rajat.wallet.dto.ErrorResponse;
import java.sql.SQLException;
import org.junit.jupiter.api.Test;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

/**
 * Unit tests for the one piece of branching logic in the handler: a {@code DataIntegrityViolation}
 * is only a retryable {@code 409} when it is the idempotency-key unique violation; every other
 * integrity violation must surface as {@code 500} so it is not misdiagnosed as a duplicate.
 */
class GlobalExceptionHandlerTest {

  private final GlobalExceptionHandler handler = new GlobalExceptionHandler();

  @Test
  void idempotencyKeyUniqueViolationMapsToConflict() {
    DataIntegrityViolationException ex =
        new DataIntegrityViolationException(
            "could not execute statement",
            new SQLException(
                "ERROR: duplicate key value violates unique constraint"
                    + " \"idempotency_records_key_unique\""));

    ResponseEntity<ErrorResponse> response = handler.handleDataIntegrity(ex);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CONFLICT);
  }

  @Test
  void otherIntegrityViolationMapsToInternalServerError() {
    DataIntegrityViolationException ex =
        new DataIntegrityViolationException(
            "could not execute statement",
            new SQLException(
                "ERROR: insert or update on table \"ledger_entries\" violates foreign key"
                    + " constraint \"fk_ledger_transfer\""));

    ResponseEntity<ErrorResponse> response = handler.handleDataIntegrity(ex);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.INTERNAL_SERVER_ERROR);
  }
}
