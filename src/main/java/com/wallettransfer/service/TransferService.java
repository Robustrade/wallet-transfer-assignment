package com.wallettransfer.service;

import com.wallettransfer.domain.FailureReason;
import com.wallettransfer.domain.IdempotencyKeyReusedException;
import com.wallettransfer.domain.IdempotencyRecord;
import com.wallettransfer.domain.LedgerEntry;
import com.wallettransfer.domain.RequestHash;
import com.wallettransfer.domain.Transfer;
import com.wallettransfer.domain.TransferNotFoundException;
import com.wallettransfer.domain.WalletNotFoundException;
import com.wallettransfer.repository.IdempotencyRepository;
import com.wallettransfer.repository.LedgerRepository;
import com.wallettransfer.repository.TransferRepository;
import com.wallettransfer.repository.WalletRepository;
import java.time.Clock;
import java.time.Instant;
import java.util.List;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

/** The transfer workflow. Each public method is one database transaction. */
@Service
public class TransferService {

  private static final Logger LOG = LoggerFactory.getLogger(TransferService.class);

  private final WalletRepository wallets;
  private final TransferRepository transfers;
  private final LedgerRepository ledger;
  private final IdempotencyRepository idempotency;
  private final Clock clock;
  private final IdGenerator ids;

  TransferService(
      WalletRepository wallets,
      TransferRepository transfers,
      LedgerRepository ledger,
      IdempotencyRepository idempotency,
      Clock clock,
      IdGenerator ids) {
    this.wallets = wallets;
    this.transfers = transfers;
    this.ledger = ledger;
    this.idempotency = idempotency;
    this.clock = clock;
    this.ids = ids;
  }

  /**
   * Validates and executes a transfer atomically. A business failure (insufficient funds) is a
   * recorded FAILED transfer, not an exception, so that it is durable.
   *
   * <p>With an idempotency key the request is executed at most once: the key is claimed in the same
   * transaction as the transfer, and any later request with the key gets the stored result back.
   */
  @Transactional
  public TransferResult createTransfer(CreateTransferCommand command) {
    Instant now = clock.instant();
    Transfer transfer =
        Transfer.create(
            ids.newId(),
            normalize(command.idempotencyKey()),
            command.fromWalletId(),
            command.toWalletId(),
            command.amount(),
            now);

    String requestHash =
        RequestHash.of(command.fromWalletId(), command.toWalletId(), command.amount());
    if (transfer.idempotencyKey() != null
        && !idempotency.tryClaim(
            new IdempotencyRecord(transfer.idempotencyKey(), requestHash, transfer.id(), now))) {
      return replay(transfer.idempotencyKey(), requestHash);
    }

    lockWallets(transfer);
    transfers.insert(transfer);

    if (!wallets.debitIfSufficient(transfer.fromWalletId(), transfer.amount(), now)) {
      Transfer failed = transfer.failed(FailureReason.INSUFFICIENT_FUNDS, now);
      transfers.updateState(failed);
      LOG.info("transfer failed id={} reason={}", failed.id(), failed.failureReason());
      return new TransferResult(failed, false);
    }
    wallets.credit(transfer.toWalletId(), transfer.amount(), now);
    ledger.insert(
        LedgerEntry.debit(transfer, ids.newId(), now),
        LedgerEntry.credit(transfer, ids.newId(), now));
    Transfer processed = transfer.processed(now);
    transfers.updateState(processed);
    LOG.info("transfer processed id={} amount={}", processed.id(), processed.amount());
    return new TransferResult(processed, false);
  }

  @Transactional(readOnly = true)
  public Transfer getTransfer(String id) {
    return transfers.findById(id).orElseThrow(() -> new TransferNotFoundException(id));
  }

  /** Another request owns the key (and has committed): return its result without side effects. */
  private TransferResult replay(String key, String requestHash) {
    IdempotencyRecord original =
        idempotency
            .findByKey(key)
            .orElseThrow(() -> new IllegalStateException("claimed key has no record: " + key));
    if (!original.requestHash().equals(requestHash)) {
      throw new IdempotencyKeyReusedException(key);
    }
    Transfer transfer = getTransfer(original.transferId());
    LOG.info("transfer replayed id={} key={} state={}", transfer.id(), key, transfer.state());
    return new TransferResult(transfer, true);
  }

  private static String normalize(String key) {
    return key == null || key.isBlank() ? null : key;
  }

  /** Locks both wallets in id order; the transfer cannot proceed if either does not exist. */
  private void lockWallets(Transfer transfer) {
    List<String> locked = wallets.lockForUpdate(transfer.fromWalletId(), transfer.toWalletId());
    for (String id : List.of(transfer.fromWalletId(), transfer.toWalletId())) {
      if (!locked.contains(id)) {
        throw new WalletNotFoundException(id);
      }
    }
  }
}
