package com.wallettransfer.controller;

import com.wallettransfer.constants.ApiConstants;
import com.wallettransfer.constants.ResponseConstant;
import com.wallettransfer.dto.requestdto.TransferHistoryRequestDTO;
import com.wallettransfer.dto.requestdto.WalletBalanceRequestDTO;
import com.wallettransfer.dto.responsedto.ResponseDTO;
import com.wallettransfer.service.loggerservice.LogService;
import com.wallettransfer.service.walletservice.WalletService;
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
    name = "Wallet APIs",
    description = "Read-only wallet operations for balance lookup and transfer history.")
public class WalletController {

  private final String className = this.getClass().getSimpleName();
  private final LogService logService;
  private final HttpServletRequest request;
  private final WalletService walletService;

  public WalletController(
      LogService logService, HttpServletRequest request, WalletService walletService) {
    this.logService = logService;
    this.request = request;
    this.walletService = walletService;
  }

  @PostMapping("wallet-balance")
  @Operation(
      summary = "Get wallet balance",
      description =
          """
          Returns the current stored balance for a wallet.

          Balances are updated atomically during transfer processing and protected by pessimistic \
          row locks to remain correct under concurrent debits.
          """)
  @ApiResponse(
      responseCode = ResponseConstant.BALANCE_FETCH_SUCCESS,
      description = "Balance fetched successfully. `data` contains `walletId` and `balance`.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  @ApiResponse(
      responseCode = ResponseConstant.WALLET_NOT_FOUND,
      description = "Wallet does not exist.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  @ApiResponse(
      responseCode = ResponseConstant.INVALID_REQUEST,
      description = "Validation failed — e.g. missing walletId.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  @ApiResponse(
      responseCode = ResponseConstant.FAILURE_CODE,
      description = "Unexpected server error.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  public ResponseDTO getWalletBalance(@Valid @RequestBody WalletBalanceRequestDTO balanceRequest) {
    logService.printInfoLogs(request, className, "Get Wallet Balance");
    return walletService.getWalletBalance(balanceRequest);
  }

  @PostMapping("transfer-history")
  @Operation(
      summary = "Get transfer history for a wallet",
      description =
          """
          Returns transfer details for all transfers that have ledger entries involving the given wallet.

          Each item includes transfer metadata, status, and the full set of ledger entries for that transfer \
          (DEBIT and CREDIT), ordered by most recent activity first.
          """)
  @ApiResponse(
      responseCode = ResponseConstant.HISTORY_FETCH_SUCCESS,
      description =
          "Transfer history fetched successfully. `data` is a list of transfer responses.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  @ApiResponse(
      responseCode = ResponseConstant.WALLET_NOT_FOUND,
      description = "Wallet does not exist.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  @ApiResponse(
      responseCode = ResponseConstant.INVALID_REQUEST,
      description = "Validation failed — e.g. missing walletId.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  @ApiResponse(
      responseCode = ResponseConstant.FAILURE_CODE,
      description = "Unexpected server error.",
      content = @Content(schema = @Schema(implementation = ResponseDTO.class)))
  public ResponseDTO getTransferHistory(
      @Valid @RequestBody TransferHistoryRequestDTO historyRequest) {
    logService.printInfoLogs(request, className, "Get Transfer History");
    return walletService.getTransferHistory(historyRequest);
  }
}
