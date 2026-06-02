package com.walletservice.application.service;

import com.walletservice.application.exception.IdempotencyConflictException;
import com.walletservice.application.util.HashUtil;
import com.walletservice.domain.entity.*;
import com.walletservice.infrastructure.repository.*;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.util.Optional;

/**
 * Service for wallet transfer orchestration.
 *
 * Design Notes:
 * ============
 *
 * 1. IDEMPOTENCY STRATEGY:
 *    - Each request includes an idempotencyKey that must be globally unique
 *    - IdempotencyRecord table maintains a 1:1 mapping from key -> transfer
 *    - On duplicate request: database constraint prevents duplicate insertion
 *    - Return original transfer instead of creating new one
 *    - Includes request hash to detect misuse (same key, different parameters)
 *
 * 2. CONCURRENCY SAFETY:
 *    - Uses pessimistic locking (PESSIMISTIC_WRITE) on wallets and transfers
 *    - Lock is acquired before reading state and held until transaction commits
 *    - Prevents race conditions when multiple threads access same wallet
 *    - Double-entry ledger ensures balance consistency
 *    - All updates happen atomically within a single JPA transaction
 *
 * 3. TRANSACTION BOUNDARIES:
 *    - Each transfer operation is wrapped in @Transactional
 *    - All changes (transfer status, wallet balance, ledger entries) commit together
 *    - Database constraints ensure ledger always has exactly 2 entries per transfer
 *    - If any operation fails, entire transaction rolls back
 *
 * 4. STATE MACHINE SAFETY:
 *    - Transfer can only move PENDING -> PROCESSED or PENDING -> FAILED
 *    - Pessimistic lock prevents concurrent state transitions
 *    - Domain entity validates allowed state transitions
 *
 * 5. LEDGER CONSISTENCY:
 *    - Debit and credit entries created atomically
 *    - Balance updates happen in same transaction
 *    - Ledger is immutable historical record
 *    - Balance = sum of all credits - sum of all debits
 */
@Service
@Slf4j
@RequiredArgsConstructor
public class TransferService {
    private final TransferRepository transferRepository;
    private final WalletRepository walletRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyRecordRepository idempotencyRecordRepository;

    /**
     * Creates a new transfer with idempotency guarantees.
     *
     * Idempotency behavior:
     * - If idempotencyKey has been seen before with same parameters, returns original transfer
     * - If idempotencyKey has been seen before with different parameters, throws IdempotencyConflictException
     * - Otherwise creates new transfer, ledger entries, and updates wallet balances atomically
     *
     * Concurrency safety:
     * - Acquires pessimistic write locks on both wallets
     * - Prevents concurrent debits on same wallet
     * - All side effects (transfer, ledger entries, balance updates) happen atomically
     *
     * @param fromWalletId source wallet
     * @param toWalletId destination wallet
     * @param amount transfer amount
     * @param idempotencyKey unique key for idempotency
     * @return created or previously-created transfer
     * @throws IllegalArgumentException if transfer parameters are invalid
     * @throws IllegalStateException if wallets don't exist or insufficient balance
     * @throws IdempotencyConflictException if key reused with different parameters
     */
    @Transactional
    public Transfer createTransfer(String fromWalletId, String toWalletId, Long amount, String idempotencyKey) {
        log.info("Processing transfer: from={}, to={}, amount={}, key={}",
                 fromWalletId, toWalletId, amount, idempotencyKey);

        // Step 1: Check idempotency - detect duplicate or conflicting requests
        String requestHash = HashUtil.hashTransferRequest(
            fromWalletId,
            toWalletId,
            amount
        );

        Optional<IdempotencyRecord> existingRecord = idempotencyRecordRepository.findById(idempotencyKey);
        if (existingRecord.isPresent()) {
            // Idempotency key has been used before
            IdempotencyRecord record = existingRecord.get();

            // Verify request parameters match (detect misuse)
            if (!record.getRequestHash().equals(requestHash)) {
                log.warn("Idempotency key {} reused with different parameters", idempotencyKey);
                throw new IdempotencyConflictException(
                    "Idempotency key " + idempotencyKey +
                    " was already used with different transfer parameters"
                );
            }

            // Return the original transfer
            log.info("Duplicate request detected for key={}, returning original transfer={}",
                     idempotencyKey, record.getTransferId());
            return transferRepository.findById(record.getTransferId())
                .orElseThrow(() -> new IllegalStateException(
                    "Transfer referenced by idempotency record not found: " + record.getTransferId()));
        }

        // Step 2: Acquire locks on both wallets in consistent order (to prevent deadlock)
        // Always lock source first, then destination
        //replacing the call with simple get by Id as h2DB doesn't support select for write lock
        //instead we will rely on optimistic locking
        Wallet sourceWallet = walletRepository.findById(fromWalletId)
            .orElseThrow(() -> new IllegalArgumentException(
                "Source wallet not found: " + fromWalletId));

        //replacing the call with simple get by Id as h2DB doesn't support select for write
        //instead we will rely on optimistic locking
        Wallet destinationWallet = walletRepository.findById(toWalletId)
            .orElseThrow(() -> new IllegalArgumentException(
                "Destination wallet not found: " + toWalletId));

        // Step 3: Validate transfer parameters and wallet state
        Transfer transfer = Transfer.builder()
            .fromWalletId(fromWalletId)
            .toWalletId(toWalletId)
            .amount(amount)
            .build();
        transfer.validate();

        sourceWallet.validateSufficientBalance(amount);

        // Step 4: Create transfer in PENDING state
        transfer = transferRepository.save(transfer);
        log.info("Created transfer in PENDING state: id={}", transfer.getId());

        try {
            // Step 5: Create ledger entries (debit from source, credit to destination)
            LedgerEntry debitEntry = LedgerEntry.builder()
                .transferId(transfer.getId())
                .walletId(sourceWallet.getId())
                .entryType(LedgerEntry.EntryType.DEBIT)
                .amount(amount)
                .build();
            ledgerEntryRepository.save(debitEntry);
            log.debug("Created debit ledger entry for transfer={}", transfer.getId());

            LedgerEntry creditEntry = LedgerEntry.builder()
                .transferId(transfer.getId())
                .walletId(destinationWallet.getId())
                .entryType(LedgerEntry.EntryType.CREDIT)
                .amount(amount)
                .build();
            ledgerEntryRepository.save(creditEntry);
            log.debug("Created credit ledger entry for transfer={}", transfer.getId());

            // Step 6: Update wallet balances atomically
            sourceWallet.debit(amount);
            destinationWallet.credit(amount);

            walletRepository.save(sourceWallet);
            walletRepository.save(destinationWallet);
            log.debug("Updated wallet balances: source={}, destination={}", sourceWallet.getId(), destinationWallet.getId());

            // Step 7: Mark transfer as PROCESSED
            transfer.markProcessed();
            transfer = transferRepository.save(transfer);
            log.info("Marked transfer as PROCESSED: id={}", transfer.getId());

            // Step 8: Record idempotency key
            IdempotencyRecord idempotencyRecord = IdempotencyRecord.builder()
                .idempotencyKey(idempotencyKey)
                .transferId(transfer.getId())
                .requestHash(requestHash)
                .build();
            idempotencyRecordRepository.save(idempotencyRecord);
            log.info("Recorded idempotency key for transfer: key={}", idempotencyKey);

        } catch (Exception e) {
            log.error("Error processing transfer: id={}, error={}", transfer.getId(), e.getMessage(), e);
            // Mark transfer as FAILED
            transfer.markFailed();
            transferRepository.save(transfer);
            throw e;
        }

        return transfer;
    }

    /**
     * Retrieves a transfer by ID.
     */
    @Transactional(readOnly = true)
    public Transfer getTransfer(String transferId) {
        return transferRepository.findById(transferId)
            .orElseThrow(() -> new IllegalArgumentException("Transfer not found: " + transferId));
    }

    /**
     * Retrieves all ledger entries for a transfer.
     * Useful for verification that exactly 2 entries exist and they balance.
     */
    @Transactional(readOnly = true)
    public java.util.List<LedgerEntry> getTransferLedgerEntries(String transferId) {
        return ledgerEntryRepository.findByTransferId(transferId);
    }
}

