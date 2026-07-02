package com.wallettransfer.controller;

import com.wallettransfer.constants.ApiConstants;
import com.wallettransfer.constants.ResponseConstant;
import com.wallettransfer.dto.requestdto.CreateTransferRequestDTO;
import com.wallettransfer.dto.responsedto.ResponseDTO;
import com.wallettransfer.service.loggerservice.LogService;
import com.wallettransfer.service.transferservice.TransferService;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.media.Content;
import io.swagger.v3.oas.annotations.media.Schema;
import io.swagger.v3.oas.annotations.responses.ApiResponse;
import io.swagger.v3.oas.annotations.tags.Tag;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.validation.Valid;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping(value = ApiConstants.API_VERSION_V1)
@Tag(
    name = "Transfer APIs",
    description =
        "Wallet-to-wallet transfer operations with idempotency, pessimistic locking, and double-entry ledger recording.")
public class TransferController {

  private final String className = this.getClass().getSimpleName();
  private final LogService logService;
  private final HttpServletRequest request;
  private final TransferService transferService;

  public TransferController(
      LogService logService,
      HttpServletRequest request,
      TransferService transferService) {
    this.logService = logService;
    this.request = request;
    this.transferService = transferService;
  }

  @PostMapping("transfers")
  @Operation(
      summary = "Create wallet transfer",
      description =
          """
          Executes an atomic wallet-to-wallet transfer.

          **Idempotency:** When the same `idempotencyKey` is sent with the same payload, the original \
          transfer result is returned without creating a duplicate transfer or ledger entries.

          **Concurrency:** Source and destination wallets are locked in a consistent order to prevent \
          double spending under concurrent requests.

          **Ledger:** A successful transfer creates exactly two ledger entries — one DEBIT on the source \
          wallet and one CREDIT on the destination wallet.

          **Transfer states:** PENDING → PROCESSED on success, PENDING → FAILED on business failure \
          (e.g. insufficient funds).
          """)
  @ApiResponse(
      responseCode = ResponseConstant.TRANSFER_SUCCESS,
      description =
          "Transfer processed successfully. `data.status` is PROCESSED and includes balanced ledger entries.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  @ApiResponse(
      responseCode = ResponseConstant.INSUFFICIENT_FUNDS,
      description =
          "Transfer failed due to insufficient balance. `data.status` is FAILED; no ledger entries are created.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  @ApiResponse(
      responseCode = ResponseConstant.WALLET_NOT_FOUND,
      description = "Source or destination wallet does not exist.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  @ApiResponse(
      responseCode = ResponseConstant.IDEMPOTENCY_CONFLICT,
      description = "The same idempotency key was reused with a different request payload.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  @ApiResponse(
      responseCode = ResponseConstant.INVALID_REQUEST,
      description =
          "Validation failed — e.g. missing fields, amount <= 0, or fromWalletId equals toWalletId.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  @ApiResponse(
      responseCode = ResponseConstant.FAILURE_CODE,
      description = "Unexpected server error.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  public ResponseDTO createTransfer(@Valid @RequestBody CreateTransferRequestDTO transferRequest) {
    logService.printInfoLogs(request, className, "Create Transfer");
    return transferService.createTransfer(transferRequest);
  }
}
