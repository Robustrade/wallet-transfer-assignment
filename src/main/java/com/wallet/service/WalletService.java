package com.wallet.service;

import com.wallet.domain.Transfer;
import com.wallet.domain.Wallet;
import com.wallet.exception.WalletNotFoundException;
import com.wallet.repository.TransferRepository;
import com.wallet.repository.WalletRepository;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.util.List;

/** Read-only service for wallet queries (balance, transfer history). */
@Service
public class WalletService {

    /** Default page size when the caller does not specify one. */
    public static final int DEFAULT_HISTORY_LIMIT = 100;
    /**
     * Default hard ceiling — caller-supplied limits above this are rejected as 400.
     * The runtime value is sourced from {@code wallet.transfer-history.max-limit}
     * (see {@link #maxHistoryLimit}); this constant is the fallback baked into
     * the property's default, exposed publicly only because legacy call sites
     * (and human readers) still reference it as the documented ceiling.
     */
    public static final int DEFAULT_MAX_HISTORY_LIMIT = 500;

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    /**
     * Effective ceiling for the {@code limit} query parameter, configurable via
     * {@code wallet.transfer-history.max-limit}. Configuration-driven so ops
     * can tune without redeploy when index plans or DB sizing change.
     */
    private final int maxHistoryLimit;

    public WalletService(WalletRepository walletRepository,
                         TransferRepository transferRepository,
                         @Value("${wallet.transfer-history.max-limit:" + DEFAULT_MAX_HISTORY_LIMIT + "}") int maxHistoryLimit) {
        if (maxHistoryLimit < 1) {
            throw new IllegalArgumentException(
                    "wallet.transfer-history.max-limit must be >= 1 (was " + maxHistoryLimit + ")");
        }
        this.walletRepository = walletRepository;
        this.transferRepository = transferRepository;
        this.maxHistoryLimit = maxHistoryLimit;
    }

    @Transactional(readOnly = true)
    public Wallet getWallet(String walletId) {
        return walletRepository.findById(walletId)
                .orElseThrow(() -> new WalletNotFoundException(walletId));
    }

    /**
     * Returns the most recent {@code limit} transfers for the wallet, or throws
     * if the wallet does not exist. The existence check is skipped when the
     * transfer query returns rows: a transfer cannot exist without its wallets
     * (FK constraint on {@code transfers.from_wallet_id} /
     * {@code transfers.to_wallet_id}), and wallets are append-only — see the
     * {@code COMMENT ON TABLE wallets} pinned in V3. If wallet deletion is
     * ever introduced, this short-circuit becomes a stale-data leak and must
     * be revisited.
     *
     * <p>An out-of-range {@code limit} is rejected with
     * {@link IllegalArgumentException} (mapped to 400) — silent clamping hides
     * client errors and makes paging behavior surprising. Callers must opt in
     * to the larger result set by sending a value within {@code [1, MAX]}.
     */
    @Transactional(readOnly = true)
    public List<Transfer> getTransferHistory(String walletId, int limit) {
        if (limit < 1 || limit > maxHistoryLimit) {
            throw new IllegalArgumentException(
                    "limit must be between 1 and " + maxHistoryLimit + " (was " + limit + ")");
        }
        List<Transfer> transfers = transferRepository.findByWalletId(walletId, limit);
        // FUTURE-PROOF: this short-circuit assumes wallets are NEVER deleted.
        // The invariant is pinned in V3__enforce_wallet_immutability.sql by
        // both a COMMENT ON TABLE wallets and a BEFORE DELETE trigger that
        // raises an exception. If wallet deletion (or soft-delete) is ever
        // added, an orphan transfer would let a deleted wallet's history leak
        // to any caller — at that point this branch must always run the
        // existsById check.
        if (transfers.isEmpty() && !walletRepository.existsById(walletId)) {
            throw new WalletNotFoundException(walletId);
        }
        return transfers;
    }
}
