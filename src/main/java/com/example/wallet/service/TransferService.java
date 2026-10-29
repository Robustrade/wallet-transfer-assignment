package com.example.wallet.service;

import com.example.wallet.domain.EntryType;
import com.example.wallet.domain.IdempotencyKeyReusedException;
import com.example.wallet.domain.IllegalTransferStateException;
import com.example.wallet.domain.LedgerEntry;
import com.example.wallet.domain.Transfer;
import com.example.wallet.domain.TransferNotFoundException;
import com.example.wallet.domain.Wallet;
import com.example.wallet.domain.WalletNotFoundException;
import com.example.wallet.repository.LedgerRepository;
import com.example.wallet.repository.TransferRepository;
import com.example.wallet.repository.WalletRepository;
import java.util.List;
import java.util.UUID;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class TransferService {

    static final String INSUFFICIENT_FUNDS = "INSUFFICIENT_FUNDS";

    private static final Logger log = LoggerFactory.getLogger(TransferService.class);

    private final WalletRepository wallets;
    private final TransferRepository transfers;
    private final LedgerRepository ledger;

    public TransferService(WalletRepository wallets, TransferRepository transfers, LedgerRepository ledger) {
        this.wallets = wallets;
        this.transfers = transfers;
        this.ledger = ledger;
    }

    /**
     * Executes a transfer exactly once per idempotency key.
     *
     * <p>Everything below runs in ONE database transaction (READ COMMITTED + explicit row locks):
     *
     * <ol>
     *   <li>Claim the idempotency key by inserting a PENDING transfer (unique constraint). If the key
     *       exists, return the stored result instead of doing any work.
     *   <li>Lock both wallet rows in id order (no deadlocks, no double spend).
     *   <li>Insufficient funds: mark FAILED (committed, so retries see the same answer).
     *   <li>Otherwise move the money, write the DEBIT and CREDIT ledger entries and mark PROCESSED.
     * </ol>
     *
     * Unknown wallets and other unexpected errors roll back the whole transaction including the
     * claimed key, so a retry after fixing the problem starts clean.
     */
    @Transactional
    public TransferResult createTransfer(CreateTransferCommand cmd) {
        String hash = RequestFingerprint.of(cmd);
        Transfer pending = Transfer.pending(
                cmd.idempotencyKey(), hash, cmd.fromWalletId(), cmd.toWalletId(), cmd.amount());

        if (!transfers.insertPendingIfKeyAbsent(pending)) {
            return replay(cmd.idempotencyKey(), hash);
        }

        Wallet source = lockAndGetSource(cmd);

        if (!source.canCover(cmd.amount())) {
            Transfer failed = pending.markFailed(INSUFFICIENT_FUNDS);
            applyTransition(pending, failed);
            log.info("transfer {} FAILED reason={} wallet={}", failed.id(), INSUFFICIENT_FUNDS, source.id());
            return new TransferResult(load(failed.id()), false);
        }

        if (!wallets.debit(cmd.fromWalletId(), cmd.amount())) {
            // Unreachable while we hold the row lock; kept as a safety net, rolls everything back.
            throw new IllegalStateException("Guarded debit failed for wallet " + cmd.fromWalletId());
        }
        wallets.credit(cmd.toWalletId(), cmd.amount());
        ledger.insert(cmd.fromWalletId(), pending.id(), EntryType.DEBIT, cmd.amount());
        ledger.insert(cmd.toWalletId(), pending.id(), EntryType.CREDIT, cmd.amount());

        applyTransition(pending, pending.markProcessed());
        log.info("transfer {} PROCESSED amount={}", pending.id(), cmd.amount());
        return new TransferResult(load(pending.id()), false);
    }

    @Transactional(readOnly = true)
    public Transfer getTransfer(UUID id) {
        return transfers.findById(id).orElseThrow(() -> new TransferNotFoundException(id));
    }

    @Transactional(readOnly = true)
    public List<Transfer> getTransfersForWallet(String walletId, int limit) {
        wallets.findById(walletId).orElseThrow(() -> new WalletNotFoundException(walletId));
        return transfers.findByWallet(walletId, limit);
    }

    private TransferResult replay(String key, String hash) {
        // The insert above waited for any in-flight transaction with this key, so the row is final.
        Transfer existing = transfers.findByIdempotencyKey(key)
                .orElseThrow(() -> new IllegalStateException("Idempotency key vanished: " + key));
        if (!existing.requestHash().equals(hash)) {
            throw new IdempotencyKeyReusedException(key);
        }
        log.info("transfer {} replayed for idempotency key", existing.id());
        return new TransferResult(existing, true);
    }

    private Wallet lockAndGetSource(CreateTransferCommand cmd) {
        List<Wallet> locked = wallets.lockBothOrderedById(cmd.fromWalletId(), cmd.toWalletId());
        Wallet source = locked.stream().filter(w -> w.id().equals(cmd.fromWalletId())).findFirst()
                .orElseThrow(() -> new WalletNotFoundException(cmd.fromWalletId()));
        locked.stream().filter(w -> w.id().equals(cmd.toWalletId())).findFirst()
                .orElseThrow(() -> new WalletNotFoundException(cmd.toWalletId()));
        return source;
    }

    private void applyTransition(Transfer from, Transfer to) {
        if (!transfers.transition(from.id(), from.status(), to.status(), to.failureReason())) {
            throw new IllegalTransferStateException(from.id(), from.status(), to.status());
        }
    }

    private Transfer load(UUID id) {
        return transfers.findById(id).orElseThrow(() -> new TransferNotFoundException(id));
    }

    /** Exposed for tests/diagnostics: entries recorded for a transfer. */
    @Transactional(readOnly = true)
    public List<LedgerEntry> getLedgerEntries(UUID transferId) {
        return ledger.findByTransferId(transferId);
    }
}
