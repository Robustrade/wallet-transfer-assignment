package com.rajat.wallet;

import static org.assertj.core.api.Assertions.assertThat;

import com.rajat.wallet.domain.entities.LedgerEntry;
import com.rajat.wallet.domain.entities.Transfer;
import com.rajat.wallet.domain.enums.EntryType;
import com.rajat.wallet.domain.enums.TransferStatus;
import com.rajat.wallet.dto.CreateTransferRequest;
import com.rajat.wallet.dto.TransferResponse;
import com.rajat.wallet.support.AbstractIntegrationTest;
import java.util.List;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

/**
 * End-to-end behaviour of {@code POST /transfers}: transfer execution, double-entry ledger
 * correctness, the FAILED outcomes, and transport validation. Behavioural assertions only — they go
 * through HTTP and inspect committed state, never internal calls.
 */
class TransferApiIT extends AbstractIntegrationTest {

  @Test
  void successfulTransferMovesFundsWritesTwoLedgerEntriesAndReturns201() {
    UUID source = seedWallet("100.00", "INR");
    UUID dest = seedWallet("0.00", "INR");

    ResponseEntity<TransferResponse> response =
        postTransfer(transfer("key-happy", source, dest, "30.00"));

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CREATED);
    TransferResponse body = response.getBody();
    assertThat(body).isNotNull();
    assertThat(body.status()).isEqualTo(TransferStatus.PROCESSED);
    assertThat(body.transferId()).isNotNull();

    // Balances moved by exactly the transfer amount.
    assertThat(balanceOf(source)).isEqualByComparingTo("70.00");
    assertThat(balanceOf(dest)).isEqualByComparingTo("30.00");

    // Exactly two ledger entries, and the ledger balances (debit total == credit total).
    List<LedgerEntry> entries = ledgerEntryRepository.findAll();
    assertThat(entries).hasSize(2);

    LedgerEntry debit =
        entries.stream().filter(e -> e.getType() == EntryType.DEBIT).findFirst().orElseThrow();
    LedgerEntry credit =
        entries.stream().filter(e -> e.getType() == EntryType.CREDIT).findFirst().orElseThrow();

    assertThat(debit.getWalletId()).isEqualTo(source);
    assertThat(debit.getTransferId()).isEqualTo(body.transferId());
    assertThat(debit.getAmount()).isEqualByComparingTo("30.00");
    assertThat(debit.getBalanceAfter()).isEqualByComparingTo("70.00");

    assertThat(credit.getWalletId()).isEqualTo(dest);
    assertThat(credit.getTransferId()).isEqualTo(body.transferId());
    assertThat(credit.getAmount()).isEqualByComparingTo("30.00");
    assertThat(credit.getBalanceAfter()).isEqualByComparingTo("30.00");
  }

  @Test
  void insufficientFundsReturns422FailedAndDoesNotMoveMoney() {
    UUID source = seedWallet("10.00", "INR");
    UUID dest = seedWallet("0.00", "INR");

    ResponseEntity<TransferResponse> response =
        postTransfer(transfer("key-insufficient", source, dest, "30.00"));

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
    assertThat(response.getBody()).isNotNull();
    assertThat(response.getBody().status()).isEqualTo(TransferStatus.FAILED);
    assertThat(response.getBody().failureReason()).containsIgnoringCase("insufficient");

    // No money moved and no ledger entries were written.
    assertThat(balanceOf(source)).isEqualByComparingTo("10.00");
    assertThat(balanceOf(dest)).isEqualByComparingTo("0.00");
    assertThat(ledgerEntryRepository.count()).isZero();

    // The failed attempt is still recorded as a transfer in the FAILED state.
    List<Transfer> transfers = transferRepository.findAll();
    assertThat(transfers).hasSize(1);
    assertThat(transfers.get(0).getStatus()).isEqualTo(TransferStatus.FAILED);
  }

  @Test
  void currencyMismatchReturns422FailedAndDoesNotMoveMoney() {
    UUID source = seedWallet("100.00", "INR");
    UUID dest = seedWallet("0.00", "USD");

    ResponseEntity<TransferResponse> response =
        postTransfer(transfer("key-currency", source, dest, "30.00"));

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
    assertThat(response.getBody()).isNotNull();
    assertThat(response.getBody().status()).isEqualTo(TransferStatus.FAILED);
    assertThat(response.getBody().failureReason()).containsIgnoringCase("currency");

    assertThat(balanceOf(source)).isEqualByComparingTo("100.00");
    assertThat(balanceOf(dest)).isEqualByComparingTo("0.00");
    assertThat(ledgerEntryRepository.count()).isZero();
  }

  @Test
  void unknownWalletReturns404() {
    UUID dest = seedWallet("0.00", "INR");
    CreateTransferRequest request = transfer("key-unknown", UUID.randomUUID(), dest, "30.00");

    ResponseEntity<String> response = postForString(request);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    // Nothing was persisted — the reservation rolled back with the transaction.
    assertThat(transferRepository.count()).isZero();
    assertThat(idempotencyRepository.count()).isZero();
  }

  @Test
  void blankIdempotencyKeyIsRejectedWith400() {
    UUID source = seedWallet("100.00", "INR");
    UUID dest = seedWallet("0.00", "INR");

    ResponseEntity<String> response = postForString(transfer("", source, dest, "30.00"));

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
  }

  @Test
  void nonPositiveAmountIsRejectedWith400() {
    ResponseEntity<String> response =
        postForString(transfer("key-neg", UUID.randomUUID(), UUID.randomUUID(), "-5.00"));

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
  }

  @Test
  void moreThanTwoDecimalPlacesIsRejectedWith400() {
    ResponseEntity<String> response =
        postForString(transfer("key-scale", UUID.randomUUID(), UUID.randomUUID(), "30.123"));

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
  }

  @Test
  void selfTransferIsRejectedWith400() {
    UUID wallet = UUID.randomUUID();

    ResponseEntity<String> response = postForString(transfer("key-self", wallet, wallet, "30.00"));

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
  }

  @Test
  void malformedJsonBodyIsRejectedWith400() {
    ResponseEntity<String> response = postJson("{ not valid json ");

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
  }
}
