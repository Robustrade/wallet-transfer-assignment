package com.wallettransfer.web;

import com.wallettransfer.domain.TransferState;
import com.wallettransfer.service.TransferResult;
import com.wallettransfer.service.TransferService;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

/** HTTP surface of the transfer workflow. */
@RestController
public class TransferController {

  static final String REPLAYED_HEADER = "Idempotent-Replayed";

  private final TransferService transfers;

  public TransferController(TransferService transfers) {
    this.transfers = transfers;
  }

  /** 201 processed, 200 replay of a processed transfer, 422 when the transfer FAILED. */
  @PostMapping("/transfers")
  public ResponseEntity<TransferResponse> create(@RequestBody CreateTransferRequest request) {
    TransferResult result = transfers.createTransfer(request.toCommand());

    ResponseEntity.BodyBuilder response = ResponseEntity.status(statusOf(result));
    if (result.replayed()) {
      response.header(REPLAYED_HEADER, "true");
    }
    return response.body(TransferResponse.from(result.transfer()));
  }

  /** The stored transfer, whatever its state; 404 when it does not exist. */
  @GetMapping("/transfers/{id}")
  public TransferResponse get(@PathVariable String id) {
    return TransferResponse.from(transfers.getTransfer(id));
  }

  private static HttpStatus statusOf(TransferResult result) {
    if (result.transfer().state() == TransferState.FAILED) {
      return HttpStatus.UNPROCESSABLE_ENTITY;
    }
    return result.replayed() ? HttpStatus.OK : HttpStatus.CREATED;
  }
}
