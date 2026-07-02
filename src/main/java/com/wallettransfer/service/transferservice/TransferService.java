package com.wallettransfer.service.transferservice;

import com.wallettransfer.constants.ResponseConstant;
import com.wallettransfer.dto.requestdto.CreateTransferRequestDTO;
import com.wallettransfer.dto.responsedto.ResponseDTO;
import com.wallettransfer.exception.ResponseException;
import com.wallettransfer.service.loggerservice.LogService;
import com.wallettransfer.utility.RequestHashUtility;
import com.wallettransfer.utility.ServiceResponseSupport;
import jakarta.servlet.http.HttpServletRequest;
import java.util.Objects;
import java.util.Optional;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;

@Service
public class TransferService {

  private final String className = this.getClass().getSimpleName();
  private final LogService logService;
  private final HttpServletRequest request;
  private final ServiceResponseSupport serviceResponseSupport;
  private final RequestHashUtility requestHashUtility;
  private final IdempotencyResponseLookup idempotencyResponseLookup;
  private final TransferExecutor transferExecutor;

  public TransferService(
      LogService logService,
      HttpServletRequest request,
      ServiceResponseSupport serviceResponseSupport,
      RequestHashUtility requestHashUtility,
      IdempotencyResponseLookup idempotencyResponseLookup,
      TransferExecutor transferExecutor) {
    this.logService = logService;
    this.request = request;
    this.serviceResponseSupport = serviceResponseSupport;
    this.requestHashUtility = requestHashUtility;
    this.idempotencyResponseLookup = idempotencyResponseLookup;
    this.transferExecutor = transferExecutor;
  }

  public ResponseDTO createTransfer(CreateTransferRequestDTO transferRequest) {
    logService.printInfoLogs(request, className, "Create Transfer");
    return serviceResponseSupport.execute(className, () -> processTransfer(transferRequest));
  }

  private ResponseDTO processTransfer(CreateTransferRequestDTO transferRequest) {
    validateTransferRequest(transferRequest);
    String requestHash = requestHashUtility.hashTransferRequest(transferRequest);
    String idempotencyKey = transferRequest.getIdempotencyKey();

    return idempotencyResponseLookup
        .findCachedResponse(idempotencyKey, requestHash)
        .orElseGet(
            () -> executeTransferWithIntegrityRetry(transferRequest, requestHash, idempotencyKey));
  }

  private ResponseDTO executeTransferWithIntegrityRetry(
      CreateTransferRequestDTO transferRequest, String requestHash, String idempotencyKey) {
    try {
      return transferExecutor.executeTransfer(transferRequest, requestHash);
    } catch (DataIntegrityViolationException integrityViolation) {
      return lookupCachedResponseWithRetry(idempotencyKey, requestHash)
          .orElseThrow(() -> integrityViolation);
    }
  }

  private Optional<ResponseDTO> lookupCachedResponseWithRetry(
      String idempotencyKey, String requestHash) {
    for (int attempt = 0; attempt < 20; attempt++) {
      Optional<ResponseDTO> cached =
          idempotencyResponseLookup.findCachedResponse(idempotencyKey, requestHash);
      if (cached.isPresent()) {
        return cached;
      }
      sleepBriefly();
    }
    return Optional.empty();
  }

  private void validateTransferRequest(CreateTransferRequestDTO transferRequest) {
    if (Objects.equals(transferRequest.getFromWalletId(), transferRequest.getToWalletId())) {
      throw new ResponseException(
          "fromWalletId and toWalletId must be different", ResponseConstant.INVALID_REQUEST);
    }
  }

  private void sleepBriefly() {
    try {
      Thread.sleep(25);
    } catch (InterruptedException interruptedException) {
      Thread.currentThread().interrupt();
    }
  }
}
