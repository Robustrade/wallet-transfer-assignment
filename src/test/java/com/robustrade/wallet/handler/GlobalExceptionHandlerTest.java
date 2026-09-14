package com.robustrade.wallet.handler;

import static org.assertj.core.api.Assertions.assertThat;

import com.robustrade.wallet.handler.dto.ErrorResponse;
import org.junit.jupiter.api.Test;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

class GlobalExceptionHandlerTest {

    private final GlobalExceptionHandler handler = new GlobalExceptionHandler();

    @Test
    void handleDataIntegrity_whenWalletUserIdUnique_returnsConflict() {
        DataIntegrityViolationException ex = new DataIntegrityViolationException(
                "could not execute statement",
                new RuntimeException(
                        "Unique index or primary key violation: \"public.wallets_user_id_uk\""
                )
        );

        ResponseEntity<ErrorResponse> response = handler.handleDataIntegrity(ex);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CONFLICT);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().getErrorCode()).isEqualTo("WALLET_ALREADY_EXISTS");
        assertThat(response.getBody().getMessage()).contains("Wallet already exists");
    }

    @Test
    void handleDataIntegrity_whenTransferIdempotencyUnique_returnsConflict() {
        DataIntegrityViolationException ex = new DataIntegrityViolationException(
                "could not execute statement",
                new RuntimeException(
                        "Unique index or primary key violation: \"public.transfers_idempotency_key_uk\""
                )
        );

        ResponseEntity<ErrorResponse> response = handler.handleDataIntegrity(ex);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CONFLICT);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().getErrorCode()).isEqualTo("IDEMPOTENCY_CONFLICT");
    }

    @Test
    void handleDataIntegrity_whenUnknownConstraint_returnsGenericConflict() {
        DataIntegrityViolationException ex = new DataIntegrityViolationException(
                "could not execute statement",
                new RuntimeException("Some other constraint failed")
        );

        ResponseEntity<ErrorResponse> response = handler.handleDataIntegrity(ex);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CONFLICT);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().getErrorCode()).isEqualTo("DATA_CONFLICT");
    }

    @Test
    void handleUnexpected_returnsOpaqueInternalError() {
        ResponseEntity<ErrorResponse> response =
                handler.handleUnexpected(new RuntimeException("secret sql detail"));

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.INTERNAL_SERVER_ERROR);
        assertThat(response.getBody()).isNotNull();
        assertThat(response.getBody().getErrorCode()).isEqualTo("INTERNAL_ERROR");
        assertThat(response.getBody().getMessage()).isEqualTo("An unexpected error occurred");
        assertThat(response.getBody().getMessage()).doesNotContain("secret");
    }
}
