package com.wallettransfer.service.transferservice;

import com.wallettransfer.builder.ResponseBuilderFactory;
import com.wallettransfer.constants.ResponseConstant;
import com.wallettransfer.dto.responsedto.ResponseDTO;
import com.wallettransfer.dto.responsedto.TransferResponseDTO;
import com.wallettransfer.entity.IdempotencyRecord;
import com.wallettransfer.entity.LedgerEntry;
import com.wallettransfer.entity.Transfer;
import com.wallettransfer.enums.TransferStatus;
import com.wallettransfer.exception.ResponseException;
import com.wallettransfer.repo.IdempotencyRecordRepository;
import com.wallettransfer.repo.LedgerEntryRepository;
import com.wallettransfer.repo.TransferRepository;
import com.wallettransfer.utility.RequestHashUtility;
import com.wallettransfer.utility.TransferMapper;
import java.util.List;
import java.util.Optional;
import org.springframework.stereotype.Component;
import org.springframework.transaction.annotation.Propagation;
import org.springframework.transaction.annotation.Transactional;

@Component
public class IdempotencyResponseLookup {

  private final TransferRepository transferRepository;
  private final LedgerEntryRepository ledgerEntryRepository;
  private final IdempotencyRecordRepository idempotencyRecordRepository;
  private final RequestHashUtility requestHashUtility;

  public IdempotencyResponseLookup(
      TransferRepository transferRepository,
      LedgerEntryRepository ledgerEntryRepository,
      IdempotencyRecordRepository idempotencyRecordRepository,
      RequestHashUtility requestHashUtility) {
    this.transferRepository = transferRepository;
    this.ledgerEntryRepository = ledgerEntryRepository;
    this.idempotencyRecordRepository = idempotencyRecordRepository;
    this.requestHashUtility = requestHashUtility;
  }

  @Transactional(propagation = Propagation.REQUIRES_NEW, readOnly = true)
  public Optional<ResponseDTO> findCachedResponse(String idempotencyKey, String requestHash) {
    Optional<IdempotencyRecord> idempotencyRecord =
        idempotencyRecordRepository.findByIdempotencyKey(idempotencyKey);
    if (idempotencyRecord.isPresent()) {
      return Optional.of(
          buildResponseFromIdempotencyRecord(idempotencyRecord.orElseThrow(), requestHash));
    }

    return transferRepository
        .findByIdempotencyKey(idempotencyKey)
        .map(this::buildResponseFromExistingTransfer);
  }

  private ResponseDTO buildResponseFromIdempotencyRecord(
      IdempotencyRecord idempotencyRecord, String requestHash) {
    if (!idempotencyRecord.getRequestHash().equals(requestHash)) {
      throw new ResponseException(
          "Idempotency key reused with different request payload",
          ResponseConstant.IDEMPOTENCY_CONFLICT);
    }
    TransferResponseDTO payload =
        requestHashUtility.fromJson(
            idempotencyRecord.getResponsePayload(), TransferResponseDTO.class);
    return ResponseDTO.builder()
        .status(idempotencyRecord.getResponseStatus())
        .message(idempotencyRecord.getResponseMessage())
        .responseCode(idempotencyRecord.getResponseCode())
        .data(payload)
        .build();
  }

  private ResponseDTO buildResponseFromExistingTransfer(Transfer transfer) {
    Transfer terminalTransfer = awaitTerminalTransfer(transfer);
    List<LedgerEntry> ledgerEntries =
        ledgerEntryRepository.findByTransfer_Id(terminalTransfer.getId());
    TransferResponseDTO payload =
        TransferMapper.toTransferResponse(terminalTransfer, ledgerEntries);
    String message =
        terminalTransfer.getStatus() == TransferStatus.PROCESSED
            ? "Transfer processed successfully"
            : "Transfer failed";
    String responseCode =
        terminalTransfer.getStatus() == TransferStatus.PROCESSED
            ? ResponseConstant.TRANSFER_SUCCESS
            : ResponseConstant.TRANSFER_FAILED;
    return ResponseBuilderFactory.getResponse(message, responseCode, payload);
  }

  private Transfer awaitTerminalTransfer(Transfer transfer) {
    if (transfer.getStatus() != TransferStatus.PENDING) {
      return transfer;
    }

    for (int attempt = 0; attempt < 20; attempt++) {
      sleepBriefly();
      Transfer refreshed =
          transferRepository.findById(transfer.getId()).orElse(transfer);
      if (refreshed.getStatus() != TransferStatus.PENDING) {
        return refreshed;
      }
    }

    throw new ResponseException(
        "Transfer is still in progress for idempotency key " + transfer.getIdempotencyKey(),
        ResponseConstant.IDEMPOTENCY_CONFLICT);
  }

  private void sleepBriefly() {
    try {
      Thread.sleep(25);
    } catch (InterruptedException interruptedException) {
      Thread.currentThread().interrupt();
    }
  }
}
