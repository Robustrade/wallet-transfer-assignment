package com.rajat.wallet.controller;

import com.rajat.wallet.domain.enums.TransferStatus;
import com.rajat.wallet.dto.CreateTransferRequest;
import com.rajat.wallet.dto.TransferResponse;
import com.rajat.wallet.service.TransferService;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/**
 * HTTP entry point for transfers. Kept thin: it validates and maps transport concerns only, then
 * delegates all business logic to {@link TransferService}. Outcome-to-status mapping is the one
 * transport rule it owns — a recorded business failure (FAILED) is a 422, success is a 201 — so a
 * replayed result yields the same status code as the original request.
 */
@RestController
@RequestMapping("/transfers")
@RequiredArgsConstructor
public class TransferController {

  private final TransferService transferService;

  @PostMapping
  public ResponseEntity<TransferResponse> createTransfer(
      @Valid @RequestBody CreateTransferRequest request) {
    TransferResponse response = transferService.createTransfer(request);
    HttpStatus status =
        response.status() == TransferStatus.FAILED
            ? HttpStatus.UNPROCESSABLE_ENTITY
            : HttpStatus.CREATED;
    return ResponseEntity.status(status).body(response);
  }
}
