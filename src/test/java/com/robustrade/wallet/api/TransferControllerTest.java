package com.robustrade.wallet.api;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import com.robustrade.wallet.dto.CreateTransferRequest;
import com.robustrade.wallet.dto.TransferResponse;
import com.robustrade.wallet.entities.Transfer;
import com.robustrade.wallet.enums.FailureReason;
import com.robustrade.wallet.service.TransferResult;
import com.robustrade.wallet.service.TransferService;
import java.math.BigDecimal;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

class TransferControllerTest {

  private static final UUID CUSTOMER_ID = UUID.randomUUID();
  private static final String REPLAYED_HEADER = "Idempotent-Replayed";

  private final TransferService transferService = mock(TransferService.class);
  private final TransferController controller = new TransferController(transferService);

  @Test
  void processedTransferIsCreated() {
    Transfer transfer = pending();
    transfer.markProcessed();
    givenResult(TransferResult.executed(transfer));

    ResponseEntity<TransferResponse> response = controller.createTransfer(CUSTOMER_ID, request());

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CREATED);
    assertThat(response.getHeaders().getFirst(REPLAYED_HEADER)).isEqualTo("false");
    assertThat(response.getBody()).isNotNull();
    assertThat(response.getBody().transferId()).isEqualTo(transfer.getId());
    assertThat(response.getBody().status()).isEqualTo("PROCESSED");
  }

  @Test
  void failedTransferIsUnprocessableEntity() {
    Transfer transfer = pending();
    transfer.markFailed(FailureReason.INSUFFICIENT_FUNDS);
    givenResult(TransferResult.executed(transfer));

    ResponseEntity<TransferResponse> response = controller.createTransfer(CUSTOMER_ID, request());

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
    assertThat(response.getBody()).isNotNull();
    assertThat(response.getBody().failureReason()).isEqualTo("INSUFFICIENT_FUNDS");
  }

  /** A replay repeats the first call's status code; only the header says it repeated. */
  @Test
  void replayKeepsTheOriginalStatusAndFlagsTheHeader() {
    Transfer transfer = pending();
    transfer.markProcessed();
    givenResult(TransferResult.replayed(transfer));

    ResponseEntity<TransferResponse> response = controller.createTransfer(CUSTOMER_ID, request());

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CREATED);
    assertThat(response.getHeaders().getFirst(REPLAYED_HEADER)).isEqualTo("true");
  }

  @Test
  void replayOfAFailedTransferStaysUnprocessableEntity() {
    Transfer transfer = pending();
    transfer.markFailed(FailureReason.WALLET_NOT_ACTIVE);
    givenResult(TransferResult.replayed(transfer));

    ResponseEntity<TransferResponse> response = controller.createTransfer(CUSTOMER_ID, request());

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
    assertThat(response.getHeaders().getFirst(REPLAYED_HEADER)).isEqualTo("true");
  }

  @Test
  void pendingTransferIsCreated() {
    when(transferService.createTransfer(any(), any()))
        .thenReturn(TransferResult.executed(pending()));

    ResponseEntity<TransferResponse> response = controller.createTransfer(CUSTOMER_ID, request());

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CREATED);
  }

  private void givenResult(TransferResult result) {
    when(transferService.createTransfer(any(), any())).thenReturn(result);
  }

  private static CreateTransferRequest request() {
    return new CreateTransferRequest("key-1", "wallet_1", "wallet_2", new BigDecimal("100"));
  }

  private static Transfer pending() {
    return new Transfer("key-1", "wallet_1", "wallet_2", CUSTOMER_ID, new BigDecimal("100"));
  }
}
