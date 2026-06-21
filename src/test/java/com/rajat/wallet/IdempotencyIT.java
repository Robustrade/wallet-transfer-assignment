package com.rajat.wallet;

import static org.assertj.core.api.Assertions.assertThat;

import com.rajat.wallet.dto.TransferResponse;
import com.rajat.wallet.support.AbstractIntegrationTest;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

/**
 * Exactly-once semantics at the API level. A retried request (same key, same payload) must return
 * the original result without re-applying side effects; reusing a key for a <em>different</em>
 * payload is a client error and must be rejected rather than silently replayed.
 */
class IdempotencyIT extends AbstractIntegrationTest {

  @Test
  void replayingTheSameKeyReturnsTheOriginalResultAndAppliesTheTransferOnce() {
    UUID source = seedWallet("100.00", "INR");
    UUID dest = seedWallet("0.00", "INR");

    ResponseEntity<TransferResponse> first =
        postTransfer(transfer("retry-key", source, dest, "30.00"));
    ResponseEntity<TransferResponse> second =
        postTransfer(transfer("retry-key", source, dest, "30.00"));

    assertThat(first.getStatusCode()).isEqualTo(HttpStatus.CREATED);
    assertThat(second.getStatusCode()).isEqualTo(HttpStatus.CREATED);

    // Same logical result is replayed — identical transfer id.
    assertThat(second.getBody()).isNotNull();
    assertThat(second.getBody().transferId()).isEqualTo(first.getBody().transferId());

    // The side effects happened exactly once.
    assertThat(transferRepository.count()).isEqualTo(1);
    assertThat(ledgerEntryRepository.count()).isEqualTo(2);
    assertThat(balanceOf(source)).isEqualByComparingTo("70.00");
    assertThat(balanceOf(dest)).isEqualByComparingTo("30.00");
  }

  @Test
  void reusingAKeyForADifferentPayloadIsRejectedWith409() {
    UUID source = seedWallet("100.00", "INR");
    UUID dest = seedWallet("0.00", "INR");

    ResponseEntity<TransferResponse> first =
        postTransfer(transfer("dup-key", source, dest, "30.00"));
    assertThat(first.getStatusCode()).isEqualTo(HttpStatus.CREATED);

    // Same key, different amount -> conflict, and the original transfer is untouched.
    ResponseEntity<String> second = postForString(transfer("dup-key", source, dest, "40.00"));

    assertThat(second.getStatusCode()).isEqualTo(HttpStatus.CONFLICT);
    assertThat(transferRepository.count()).isEqualTo(1);
    assertThat(balanceOf(source)).isEqualByComparingTo("70.00");
    assertThat(balanceOf(dest)).isEqualByComparingTo("30.00");
  }
}
