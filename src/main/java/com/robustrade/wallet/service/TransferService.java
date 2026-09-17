package com.robustrade.wallet.service;

import com.robustrade.wallet.dto.CreateTransferRequest;
import com.robustrade.wallet.entities.*;
import com.robustrade.wallet.enums.FailureReason;
import com.robustrade.wallet.enums.LedgerEntryType;
import com.robustrade.wallet.exception.IdempotencyConflictException;
import com.robustrade.wallet.exception.WalletNotFoundException;
import com.robustrade.wallet.repository.LedgerEntryRepository;
import com.robustrade.wallet.repository.TransferRepository;
import com.robustrade.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import java.util.*;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class TransferService {

  private static final Logger log = LoggerFactory.getLogger(TransferService.class);

  private final WalletRepository walletRepository;
  private final TransferRepository transferRepository;
  private final LedgerEntryRepository ledgerEntryRepository;

  public TransferService(
      WalletRepository walletRepository,
      TransferRepository transferRepository,
      LedgerEntryRepository ledgerEntryRepository) {
    this.walletRepository = walletRepository;
    this.transferRepository = transferRepository;
    this.ledgerEntryRepository = ledgerEntryRepository;
  }

  /** Executes a transfer atomically. See approach.txt "Flow" for the rationale. */
  @Transactional(rollbackFor = Exception.class)
  public TransferResult createTransfer(CreateTransferRequest request, UUID initiatedBy) {
    String fromId = request.fromWalletId();
    String toId = request.toWalletId();

    log.info(
        "Transfer requested: key={}, {} -> {}, amount={}",
        request.idempotencyKey(),
        fromId,
        toId,
        request.amount());

    // A committed replay needs no wallet locks at all. A concurrent duplicate misses this
    // lookup (the other transaction's row is invisible until it commits) and falls through to
    // the ON CONFLICT insert below, where the unique index serialises the two.
    Optional<Transfer> alreadyApplied =
        transferRepository.findByIdempotencyKey(request.idempotencyKey());
    if (alreadyApplied.isPresent()) {
      return replayOf(alreadyApplied.get(), request);
    }

    LockedWallets wallets = lockInDeadlockSafeOrder(fromId, toId);
    Wallet fromWallet = wallets.from();
    Wallet toWallet = wallets.to();

    int rowsInserted =
        transferRepository.insertIfAbsent(
            UUID.randomUUID(),
            request.idempotencyKey(),
            fromId,
            toId,
            initiatedBy,
            request.amount());

    Transfer transfer = loadByIdempotencyKey(request.idempotencyKey());

    if (rowsInserted == 0) {
      return replayOf(transfer, request);
    }

    if (!fromWallet.isActive() || !toWallet.isActive()) {
      log.info(
          "Transfer {} failed: wallet not active (from={} is {}, to={} is {})",
          transfer.getId(),
          fromWallet.getId(),
          fromWallet.getStatus(),
          toWallet.getId(),
          toWallet.getStatus());
      return failed(transfer, FailureReason.WALLET_NOT_ACTIVE);
    }

    if (!fromWallet.hasSufficientBalance(request.amount())) {
      log.info(
          "Transfer {} failed: insufficient funds in {} for amount={}",
          transfer.getId(),
          fromWallet.getId(),
          request.amount());
      return failed(transfer, FailureReason.INSUFFICIENT_FUNDS);
    }

    fromWallet.debit(request.amount());
    toWallet.credit(request.amount());
    recordDoubleEntry(transfer, fromWallet, toWallet, request.amount());
    transfer.markProcessed();

    log.info(
        "Transfer {} processed: {} -> {}, amount={}",
        transfer.getId(),
        fromWallet.getId(),
        toWallet.getId(),
        request.amount());
    return TransferResult.executed(transfer);
  }

  /**
   * Locks both rows smallest-id-first. The order of acquisition is global, so two transfers on the
   * same pair in opposite directions queue instead of deadlocking (approach.txt:89-94).
   *
   * <p>Only the order of the two {@code lockWallet} statements differs between the branches. The
   * returned pair is always (source, destination): the sort decides what gets locked first, never
   * which way the money moves.
   */
  private LockedWallets lockInDeadlockSafeOrder(String fromId, String toId) {
    if (fromId.compareTo(toId) < 0) {
      Wallet from = lockWallet(fromId);
      Wallet to = lockWallet(toId);
      return new LockedWallets(from, to);
    }
    Wallet to = lockWallet(toId);
    Wallet from = lockWallet(fromId);
    return new LockedWallets(from, to);
  }

  /** The native INSERT bypasses the persistence context, so re-read to get a managed entity. */
  private Transfer loadByIdempotencyKey(String idempotencyKey) {
    return transferRepository
        .findByIdempotencyKey(idempotencyKey)
        .orElseThrow(
            () -> new IllegalStateException("Transfer missing after insert: " + idempotencyKey));
  }

  /**
   * The key was already used. Only a request identical to the stored one is a replay; anything else
   * is a different transfer wearing a used key, which must not be silently swallowed.
   */
  private TransferResult replayOf(Transfer transfer, CreateTransferRequest request) {
    if (!transfer.matches(request.fromWalletId(), request.toWalletId(), request.amount())) {
      throw new IdempotencyConflictException(request.idempotencyKey());
    }
    log.info(
        "Idempotent replay: key={} already applied as transfer={} with status={}",
        request.idempotencyKey(),
        transfer.getId(),
        transfer.getStatus());
    return TransferResult.replayed(transfer);
  }

  /** Returns rather than throws, so the FAILED row commits and every retry gets the same answer. */
  private TransferResult failed(Transfer transfer, FailureReason reason) {
    transfer.markFailed(reason);
    return TransferResult.executed(transfer);
  }

  /** One row per side. The caller's transaction is what guarantees both are written. */
  private void recordDoubleEntry(
      Transfer transfer, Wallet fromWallet, Wallet toWallet, BigDecimal amount) {
    ledgerEntryRepository.save(
        new LedgerEntry(
            transfer.getId(),
            fromWallet.getCustomerId(),
            fromWallet.getId(),
            LedgerEntryType.DEBIT,
            amount));
    ledgerEntryRepository.save(
        new LedgerEntry(
            transfer.getId(),
            toWallet.getCustomerId(),
            toWallet.getId(),
            LedgerEntryType.CREDIT,
            amount));
  }

  private Wallet lockWallet(String walletId) {
    return walletRepository
        .findByIdForUpdate(walletId)
        .orElseThrow(() -> new WalletNotFoundException(walletId));
  }
}
