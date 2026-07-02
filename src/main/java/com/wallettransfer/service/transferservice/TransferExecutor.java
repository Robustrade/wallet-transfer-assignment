package com.wallettransfer.service.transferservice;

import com.wallettransfer.builder.ResponseBuilderFactory;
import com.wallettransfer.constants.ApplicationConstants;
import com.wallettransfer.constants.ResponseConstant;
import com.wallettransfer.dto.requestdto.CreateTransferRequestDTO;
import com.wallettransfer.dto.responsedto.ResponseDTO;
import com.wallettransfer.dto.responsedto.TransferResponseDTO;
import com.wallettransfer.entity.IdempotencyRecord;
import com.wallettransfer.entity.LedgerEntry;
import com.wallettransfer.entity.Transfer;
import com.wallettransfer.entity.Wallet;
import com.wallettransfer.enums.LedgerEntryType;
import com.wallettransfer.enums.TransferStatus;
import com.wallettransfer.exception.ResponseException;
import com.wallettransfer.repo.IdempotencyRecordRepository;
import com.wallettransfer.repo.LedgerEntryRepository;
import com.wallettransfer.repo.TransferRepository;
import com.wallettransfer.repo.WalletRepository;
import com.wallettransfer.utility.RequestHashUtility;
import com.wallettransfer.utility.TransferMapper;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import java.util.function.Function;
import java.util.stream.Collectors;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Component;
import org.springframework.transaction.annotation.Propagation;
import org.springframework.transaction.annotation.Transactional;

@Component
public class TransferExecutor {

  private final TransferRepository transferRepository;
  private final WalletRepository walletRepository;
  private final LedgerEntryRepository ledgerEntryRepository;
  private final IdempotencyRecordRepository idempotencyRecordRepository;
  private final RequestHashUtility requestHashUtility;

  public TransferExecutor(
      TransferRepository transferRepository,
      WalletRepository walletRepository,
      LedgerEntryRepository ledgerEntryRepository,
      IdempotencyRecordRepository idempotencyRecordRepository,
      RequestHashUtility requestHashUtility) {
    this.transferRepository = transferRepository;
    this.walletRepository = walletRepository;
    this.ledgerEntryRepository = ledgerEntryRepository;
    this.idempotencyRecordRepository = idempotencyRecordRepository;
    this.requestHashUtility = requestHashUtility;
  }

  @Transactional(propagation = Propagation.REQUIRES_NEW)
  public ResponseDTO executeTransfer(CreateTransferRequestDTO transferRequest, String requestHash) {
    List<String> walletIds =
        List.of(transferRequest.getFromWalletId(), transferRequest.getToWalletId()).stream()
            .sorted()
            .distinct()
            .toList();

    List<Wallet> lockedWallets = walletRepository.findByWalletIdsForUpdate(walletIds);
    Map<String, Wallet> walletById =
        lockedWallets.stream().collect(Collectors.toMap(Wallet::getWalletId, Function.identity()));

    Wallet fromWallet = resolveWallet(walletById, transferRequest.getFromWalletId());
    Wallet toWallet = resolveWallet(walletById, transferRequest.getToWalletId());

    Transfer transfer = createPendingTransfer(transferRequest, fromWallet, toWallet);
    transferRepository.save(transfer);

    if (fromWallet.getBalance() < transferRequest.getAmount()) {
      return failTransfer(
          transfer,
          requestHash,
          ResponseConstant.INSUFFICIENT_FUNDS,
          "Insufficient funds in wallet "
              + transferRequest.getFromWalletId()
              + ". Requested: "
              + transferRequest.getAmount()
              + ", available: "
              + fromWallet.getBalance());
    }

    applyTransfer(fromWallet, toWallet, transferRequest.getAmount());
    walletRepository.saveAll(List.of(fromWallet, toWallet));

    List<LedgerEntry> ledgerEntries =
        createLedgerEntries(transfer, fromWallet, toWallet, transferRequest.getAmount());

    transfer.markProcessed();
    transferRepository.save(transfer);

    TransferResponseDTO payload = TransferMapper.toTransferResponse(transfer, ledgerEntries);
    return persistSuccessResponse(transferRequest, requestHash, transfer, payload);
  }

  private Wallet resolveWallet(Map<String, Wallet> walletById, String walletId) {
    Wallet wallet = walletById.get(walletId);
    if (wallet == null) {
      throw new ResponseException("Wallet not found", ResponseConstant.WALLET_NOT_FOUND);
    }
    return wallet;
  }

  private Transfer createPendingTransfer(
      CreateTransferRequestDTO transferRequest, Wallet fromWallet, Wallet toWallet) {
    return Transfer.builder()
        .id(UUID.randomUUID().toString())
        .idempotencyKey(transferRequest.getIdempotencyKey())
        .fromWallet(fromWallet)
        .toWallet(toWallet)
        .amount(transferRequest.getAmount())
        .status(TransferStatus.PENDING)
        .build();
  }

  private void applyTransfer(Wallet fromWallet, Wallet toWallet, long amount) {
    fromWallet.setBalance(fromWallet.getBalance() - amount);
    toWallet.setBalance(toWallet.getBalance() + amount);
  }

  private List<LedgerEntry> createLedgerEntries(
      Transfer transfer, Wallet fromWallet, Wallet toWallet, long amount) {
    List<LedgerEntry> ledgerEntries = new ArrayList<>();
    ledgerEntries.add(
        ledgerEntryRepository.save(
            LedgerEntry.builder()
                .wallet(fromWallet)
                .transfer(transfer)
                .entryType(LedgerEntryType.DEBIT)
                .amount(amount)
                .build()));
    ledgerEntries.add(
        ledgerEntryRepository.save(
            LedgerEntry.builder()
                .wallet(toWallet)
                .transfer(transfer)
                .entryType(LedgerEntryType.CREDIT)
                .amount(amount)
                .build()));
    return ledgerEntries;
  }

  private ResponseDTO failTransfer(
      Transfer transfer, String requestHash, String responseCode, String failureMessage) {
    transfer.markFailed(failureMessage);
    transferRepository.save(transfer);

    TransferResponseDTO payload = TransferMapper.toTransferResponse(transfer, List.of());
    ResponseDTO response =
        ResponseBuilderFactory.getResponse(failureMessage, responseCode, payload);
    persistIdempotencyRecord(
        transfer.getIdempotencyKey(), transfer, requestHash, payload, response);
    return response;
  }

  private ResponseDTO persistSuccessResponse(
      CreateTransferRequestDTO transferRequest,
      String requestHash,
      Transfer transfer,
      TransferResponseDTO payload) {
    ResponseDTO response =
        ResponseBuilderFactory.getResponse(
            ApplicationConstants.SUCCESS, ResponseConstant.TRANSFER_SUCCESS, payload);
    persistIdempotencyRecord(
        transferRequest.getIdempotencyKey(), transfer, requestHash, payload, response);
    return response;
  }

  private void persistIdempotencyRecord(
      String idempotencyKey,
      Transfer transfer,
      String requestHash,
      TransferResponseDTO payload,
      ResponseDTO response) {
    try {
      idempotencyRecordRepository.save(
          IdempotencyRecord.builder()
              .idempotencyKey(idempotencyKey)
              .transfer(transfer)
              .requestHash(requestHash)
              .responseStatus(response.getStatus())
              .responseCode(response.getResponseCode())
              .responseMessage(response.getMessage())
              .responsePayload(requestHashUtility.toJson(payload))
              .build());
    } catch (DataIntegrityViolationException integrityViolation) {
      // Another concurrent request already persisted the idempotency record.
    }
  }
}
