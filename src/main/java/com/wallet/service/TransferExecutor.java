package com.wallet.service;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import com.wallet.domain.EntryType;
import com.wallet.domain.LedgerEntry;
import com.wallet.domain.Transfer;
import com.wallet.domain.Wallet;
import com.wallet.exception.WalletNotFoundException;
import com.wallet.repository.LedgerEntryRepository;
import com.wallet.repository.TransferRepository;
import com.wallet.repository.WalletRepository;

/**
 * Runs the locked portion of a transfer in its own transaction. Separated from
 * {@link TransferService} so that a {@link org.springframework.dao.DuplicateKeyException} here
 * rolls back this whole transaction (releasing the wallet locks) rather than leaving the caller
 * stuck inside an aborted transaction it can't safely query its way out of.
 */
@Service
class TransferExecutor {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;

    TransferExecutor(WalletRepository walletRepository, TransferRepository transferRepository,
            LedgerEntryRepository ledgerEntryRepository) {
        this.walletRepository = walletRepository;
        this.transferRepository = transferRepository;
        this.ledgerEntryRepository = ledgerEntryRepository;
    }

    /**
     * Locks both wallets in a globally consistent order (sorted by id, not the order the caller
     * passed them) so that two transfers moving money in opposite directions between the same
     * pair of wallets cannot deadlock. {@code lockById} throws {@link WalletNotFoundException}
     * when a wallet does not exist, so no separate existence check is needed. The transfer row is
     * inserted only after both locks are held: inserting first would make Postgres take an
     * implicit FOR KEY SHARE lock on the referenced wallet rows (to enforce the foreign key)
     * before we get our own FOR UPDATE lock, and concurrent transfers on the same pair racing
     * through that ordering deadlock on each other's shared lock.
     */
    @Transactional
    TransferResult execute(String idempotencyKey, String requestFingerprint, UUID fromWalletId, UUID toWalletId,
            BigDecimal amount) {
        boolean fromIsFirst = fromWalletId.compareTo(toWalletId) <= 0;
        Wallet firstLocked = walletRepository.lockById(fromIsFirst ? fromWalletId : toWalletId);
        Wallet secondLocked = walletRepository.lockById(fromIsFirst ? toWalletId : fromWalletId);
        Wallet source = fromIsFirst ? firstLocked : secondLocked;
        Wallet destination = fromIsFirst ? secondLocked : firstLocked;

        Transfer transfer = Transfer.createPending(UUID.randomUUID(), idempotencyKey, requestFingerprint,
                fromWalletId, toWalletId, amount, Instant.now());
        transferRepository.insertPending(transfer);

        Instant now = Instant.now();
        if (!source.hasAtLeast(amount)) {
            transfer.markFailed("insufficient funds", now);
            transferRepository.updateStatus(transfer);
            return new TransferResult(transfer, false);
        }

        walletRepository.updateBalance(source.getId(), source.getBalance().subtract(amount), now);
        walletRepository.updateBalance(destination.getId(), destination.getBalance().add(amount), now);

        ledgerEntryRepository.insert(
                new LedgerEntry(UUID.randomUUID(), transfer.getId(), source.getId(), EntryType.DEBIT, amount, now));
        ledgerEntryRepository.insert(new LedgerEntry(UUID.randomUUID(), transfer.getId(), destination.getId(),
                EntryType.CREDIT, amount, now));

        transfer.markProcessed(now);
        transferRepository.updateStatus(transfer);
        return new TransferResult(transfer, false);
    }
}
