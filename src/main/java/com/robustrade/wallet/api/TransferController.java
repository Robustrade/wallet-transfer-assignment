package com.robustrade.wallet.api;

import com.robustrade.wallet.dto.CreateTransferRequest;
import com.robustrade.wallet.dto.TransferResponse;
import com.robustrade.wallet.enums.TransferStatus;
import com.robustrade.wallet.service.TransferResult;
import com.robustrade.wallet.service.TransferService;
import jakarta.validation.Valid;
import java.util.UUID;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/transfers")
public class TransferController {

  private static final String REPLAYED_HEADER = "Idempotent-Replayed";

  private final TransferService transferService;

  public TransferController(TransferService transferService) {
    this.transferService = transferService;
  }

  /**
   * The caller is authenticated at the API gateway, which passes the customer id. The ownership
   * check (caller must own the source wallet) plugs in here and is out of scope for now.
   */
  @PostMapping
  public ResponseEntity<TransferResponse> createTransfer(
      @RequestHeader("X-Customer-Id") UUID customerId,
      @Valid @RequestBody CreateTransferRequest request) {

    TransferResult result = transferService.createTransfer(request, customerId);

    return ResponseEntity.status(statusFor(result.transfer().getStatus()))
        .header(REPLAYED_HEADER, Boolean.toString(result.replayed()))
        .body(TransferResponse.from(result.transfer()));
  }

  /**
   * A replay returns exactly what the first call returned, status code included, so the status
   * depends only on the stored transfer and never on whether this call was a replay.
   */
  private static HttpStatus statusFor(TransferStatus status) {
    return status == TransferStatus.FAILED ? HttpStatus.UNPROCESSABLE_ENTITY : HttpStatus.CREATED;
  }
}
