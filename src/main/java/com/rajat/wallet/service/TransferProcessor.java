package com.rajat.wallet.service;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.rajat.wallet.domain.entities.IdempotencyRecord;
import com.rajat.wallet.domain.entities.LedgerEntry;
import com.rajat.wallet.domain.entities.Transfer;
import com.rajat.wallet.domain.entities.Wallet;
import com.rajat.wallet.domain.enums.EntryType;
import com.rajat.wallet.domain.enums.TransferStatus;
import com.rajat.wallet.dto.CreateTransferRequest;
import com.rajat.wallet.dto.TransferResponse;
import com.rajat.wallet.exception.WalletNotFoundException;
import com.rajat.wallet.repository.IdempotencyRecordRepository;
import com.rajat.wallet.repository.LedgerEntryRepository;
import com.rajat.wallet.repository.TransferRepository;
import com.rajat.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import java.util.function.Function;
import java.util.stream.Collectors;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;
import org.springframework.transaction.annotation.Transactional;

/**
 * The atomic unit of work for a first-seen transfer: reserve the idempotency key, lock the wallets,
 * move the money, write the double-entry ledger, and cache the response — all in ONE transaction.
 *
 * <p>Kept in its own bean on purpose: that makes {@code @Transactional} a real proxy boundary. A
 * concurrent duplicate blocks on the {@code idempotency_key} unique index until this (winning)
 * transaction commits {@code COMPLETED}, then fails with a unique violation that rolls this whole
 * transaction back cleanly. {@link TransferServiceImpl} — which is not transactional, so it is not
 * poisoned — catches that and replays the committed winner instead of erroring.
 */
@Component
public class TransferProcessor {

  private static final Logger log = LoggerFactory.getLogger(TransferProcessor.class);

  // The controller maps the same way; cached here only so the registry is self-describing.
  private static final int STATUS_PROCESSED = 201;
  private static final int STATUS_FAILED = 422;

  private final WalletRepository walletRepository;
  private final TransferRepository transferRepository;
  private final LedgerEntryRepository ledgerEntryRepository;
  private final IdempotencyRecordRepository idempotencyRepository;
  private final ObjectMapper objectMapper;

  public TransferProcessor(
      WalletRepository walletRepository,
      TransferRepository transferRepository,
      LedgerEntryRepository ledgerEntryRepository,
      IdempotencyRecordRepository idempotencyRepository,
      ObjectMapper objectMapper) {
    this.walletRepository = walletRepository;
    this.transferRepository = transferRepository;
    this.ledgerEntryRepository = ledgerEntryRepository;
    this.idempotencyRepository = idempotencyRepository;
    this.objectMapper = objectMapper;
  }

  @Transactional
  public TransferResponse process(CreateTransferRequest request, String requestHash) {
    // Reserve the key and flush now, so a concurrent duplicate collides here (blocks, then fails)
    // before any money moves. On failure the whole transaction — including this insert — rolls
    // back, so the key is never left orphaned.
    IdempotencyRecord record =
        idempotencyRepository.saveAndFlush(
            IdempotencyRecord.inProgress(request.idempotencyKey(), requestHash));

    TransferResponse response = executeTransfer(request);

    int statusCode = response.status() == TransferStatus.FAILED ? STATUS_FAILED : STATUS_PROCESSED;
    record.complete(response.transferId(), statusCode, serialize(response));
    return response;
  }

  private TransferResponse executeTransfer(CreateTransferRequest request) {
    LockedWallets wallets = lockWallets(request.fromWalletId(), request.toWalletId());
    Wallet from = wallets.from();
    Wallet to = wallets.to();

    // Persist first so Hibernate assigns the UUID v7 id that the ledger entries reference.
    Transfer transfer =
        transferRepository.save(
            Transfer.pending(request.fromWalletId(), request.toWalletId(), request.amount()));

    String failureReason = validate(from, to, request.amount());
    if (failureReason != null) {
      transfer.markFailed(failureReason);
      log.info("Transfer {} FAILED: {}", transfer.getId(), failureReason);
      return TransferResponse.from(transfer);
    }

    from.debit(request.amount());
    ledgerEntryRepository.save(
        new LedgerEntry(
            from.getId(), transfer.getId(), EntryType.DEBIT, request.amount(), from.getBalance()));

    to.credit(request.amount());
    ledgerEntryRepository.save(
        new LedgerEntry(
            to.getId(), transfer.getId(), EntryType.CREDIT, request.amount(), to.getBalance()));

    transfer.markProcessed();
    log.info(
        "Transfer {} PROCESSED: {} from {} to {}",
        transfer.getId(),
        request.amount(),
        from.getId(),
        to.getId());
    return TransferResponse.from(transfer);
  }

  /**
   * Business validation under the wallet locks. Returns a failure reason, or {@code null} if ok.
   */
  private String validate(Wallet from, Wallet to, BigDecimal amount) {
    if (!from.getCurrency().equals(to.getCurrency())) {
      return "Currency mismatch: " + from.getCurrency() + " -> " + to.getCurrency();
    }
    if (!from.hasSufficientFunds(amount)) {
      return "Insufficient funds in wallet " + from.getId();
    }
    return null;
  }

  private LockedWallets lockWallets(UUID fromId, UUID toId) {
    Map<UUID, Wallet> byId =
        walletRepository.findAllForUpdate(List.of(fromId, toId)).stream()
            .collect(Collectors.toMap(Wallet::getId, Function.identity()));
    Wallet from = byId.get(fromId);
    if (from == null) {
      throw new WalletNotFoundException(fromId);
    }
    Wallet to = byId.get(toId);
    if (to == null) {
      throw new WalletNotFoundException(toId);
    }
    return new LockedWallets(from, to);
  }

  private String serialize(TransferResponse response) {
    try {
      return objectMapper.writeValueAsString(response);
    } catch (JsonProcessingException e) {
      throw new IllegalStateException("Failed to serialize transfer response", e);
    }
  }

  private record LockedWallets(Wallet from, Wallet to) {}
}
