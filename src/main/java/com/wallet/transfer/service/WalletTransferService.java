package com.wallet.transfer.service;

import com.wallet.transfer.domain.*;
import com.wallet.transfer.dto.CreateTransferRequest;
import com.wallet.transfer.dto.TransferResponse;
import com.wallet.transfer.exception.InsufficientBalanceException;
import com.wallet.transfer.exception.WalletNotFoundException;
import com.wallet.transfer.repository.*;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Isolation;
import org.springframework.transaction.annotation.Transactional;

import java.math.BigDecimal;
import java.util.UUID;

/**
 * Wallet Transfer Service
 * 
 * Orchestrates wallet-to-wallet transfers with the following guarantees:
 * 
 * 1. IDEMPOTENCY: Same idempotencyKey always produces same result
 *    - Implemented via UNIQUE constraint on idempotencyKey
 *    - Duplicate requests return cached response
 * 
 * 2. CONCURRENCY SAFETY: Safe handling of concurrent transfers
 *    - Uses SERIALIZABLE isolation level
 *    - Pessimistic row locks (SELECT ... FOR UPDATE)
 *    - Prevents race conditions on balance updates
 * 
 * 3. DOUBLE-ENTRY LEDGER: Every transfer creates 2 entries
 *    - 1 DEBIT from source wallet
 *    - 1 CREDIT to destination wallet
 *    - Ledger must always balance
 * 
 * 4. ATOMICITY: All-or-nothing transaction
 *    - Single @Transactional method boundary
 *    - Any error rolls back entire transaction
 */
@Service
@RequiredArgsConstructor
@Slf4j
public class WalletTransferService {
    
    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyRecordRepository idempotencyRecordRepository;
    
    /**
     * Execute a wallet-to-wallet transfer
     * 
     * Transaction Isolation: SERIALIZABLE
     *   - Prevents dirty reads, non-repeatable reads, phantom reads
     *   - Serializes conflicting transactions
     *   - Ensures consistent view of all data
     * 
     * Flow:
     * 1. Check idempotency: if duplicate key, return original result
     * 2. Lock source and destination wallets
     * 3. Validate balance
     * 4. Debit source wallet
     * 5. Credit destination wallet
     * 6. Create transfer record
     * 7. Create 2 ledger entries (DEBIT + CREDIT)
     * 8. Cache idempotency response
     * 9. Return response
     * 
     * @param request transfer request (idempotencyKey, fromWalletId, toWalletId, amount)
     * @return transfer response with status
     * @throws WalletNotFoundException if source or destination wallet not found
     * @throws InsufficientBalanceException if source wallet has insufficient balance
     */
    @Transactional(isolation = Isolation.SERIALIZABLE)
    public TransferResponse executeTransfer(CreateTransferRequest request) {
        log.info("Executing transfer with idempotencyKey: {}", request.getIdempotencyKey());
        
        // Step 1: Check idempotency - if duplicate key exists, return cached result
        var existingTransfer = transferRepository.findByIdempotencyKey(request.getIdempotencyKey());
        if (existingTransfer.isPresent()) {
            log.info("Duplicate transfer detected for idempotencyKey: {}. Returning cached result.", 
                     request.getIdempotencyKey());
            return toResponse(existingTransfer.get());
        }
        
        // Step 2: Lock and fetch source wallet with pessimistic lock (FOR UPDATE)
        Wallet sourceWallet = walletRepository.findByWalletIdForUpdate(request.getFromWalletId())
                .orElseThrow(() -> new WalletNotFoundException(request.getFromWalletId()));
        
        // Step 3: Lock and fetch destination wallet with pessimistic lock
        Wallet destWallet = walletRepository.findByWalletIdForUpdate(request.getToWalletId())
                .orElseThrow(() -> new WalletNotFoundException(request.getToWalletId()));
        
        // Step 4: Validate source wallet has sufficient balance
        if (sourceWallet.getBalance().compareTo(request.getAmount()) < 0) {
            throw new InsufficientBalanceException(
                request.getFromWalletId(), 
                sourceWallet.getBalance(), 
                request.getAmount()
            );
        }
        
        // Step 5: Debit source wallet
        sourceWallet.debit(request.getAmount());
        walletRepository.save(sourceWallet);
        
        // Step 6: Credit destination wallet
        destWallet.credit(request.getAmount());
        walletRepository.save(destWallet);
        
        // Step 7: Create transfer record
        Transfer transfer = Transfer.builder()
                .transferId(generateTransferId())
                .fromWalletId(request.getFromWalletId())
                .toWalletId(request.getToWalletId())
                .amount(request.getAmount())
                .idempotencyKey(request.getIdempotencyKey())
                .status(TransferStatus.PENDING)
                .build();
        
        transfer.validate();
        transfer = transferRepository.save(transfer);
        
        // Step 8: Create 2 ledger entries (DEBIT + CREDIT)
        createLedgerEntries(transfer);
        
        // Step 9: Mark transfer as PROCESSED
        transfer.markProcessed();
        transferRepository.save(transfer);
        
        // Step 10: Cache idempotency response
        cacheIdempotencyResponse(request.getIdempotencyKey(), transfer);
        
        log.info("Transfer executed successfully. TransferId: {}, Amount: {}", 
                 transfer.getTransferId(), transfer.getAmount());
        
        return toResponse(transfer);
    }
    
    /**
     * Create double-entry ledger entries for a transfer
     * 
     * Invariant: Every transfer must produce exactly 2 entries:
     *   1. DEBIT entry from source wallet
     *   2. CREDIT entry to destination wallet
     *   Both with same amount
     * 
     * @param transfer the transfer record
     */
    private void createLedgerEntries(Transfer transfer) {
        // Create DEBIT entry (source wallet)
        LedgerEntry debitEntry = LedgerEntry.builder()
                .entryId(generateEntryId())
                .transferId(transfer.getTransferId())
                .walletId(transfer.getFromWalletId())
                .entryType(LedgerEntryType.DEBIT)
                .amount(transfer.getAmount())
                .build();
        
        debitEntry.validate();
        ledgerEntryRepository.save(debitEntry);
        
        // Create CREDIT entry (destination wallet)
        LedgerEntry creditEntry = LedgerEntry.builder()
                .entryId(generateEntryId())
                .transferId(transfer.getTransferId())
                .walletId(transfer.getToWalletId())
                .entryType(LedgerEntryType.CREDIT)
                .amount(transfer.getAmount())
                .build();
        
        creditEntry.validate();
        ledgerEntryRepository.save(creditEntry);
        
        log.debug("Created ledger entries for transfer: {} (DEBIT: {}, CREDIT: {})", 
                  transfer.getTransferId(), debitEntry.getEntryId(), creditEntry.getEntryId());
    }
    
    /**
     * Cache idempotency response for duplicate request handling
     * 
     * Enables exact-once semantics: if same request is retried,
     * return this cached response
     * 
     * @param idempotencyKey the idempotency key
     * @param transfer the transfer record
     */
    private void cacheIdempotencyResponse(String idempotencyKey, Transfer transfer) {
        IdempotencyRecord record = IdempotencyRecord.builder()
                .idempotencyKey(idempotencyKey)
                .transferId(transfer.getTransferId())
                .responseStatusCode(201) // HTTP 201 Created
                .build();
        
        idempotencyRecordRepository.save(record);
    }
    
    /**
     * Convert transfer entity to response DTO
     * 
     * @param transfer the transfer entity
     * @return transfer response DTO
     */
    private TransferResponse toResponse(Transfer transfer) {
        return TransferResponse.builder()
                .transferId(transfer.getTransferId())
                .fromWalletId(transfer.getFromWalletId())
                .toWalletId(transfer.getToWalletId())
                .amount(transfer.getAmount())
                .status(transfer.getStatus().name())
                .createdAt(transfer.getCreatedAt())
                .build();
    }
    
    /**
     * Generate unique transfer ID
     * @return transfer ID
     */
    private String generateTransferId() {
        return "txn_" + UUID.randomUUID().toString().replace("-", "").substring(0, 20);
    }
    
    /**
     * Generate unique ledger entry ID
     * @return entry ID
     */
    private String generateEntryId() {
        return "led_" + UUID.randomUUID().toString().replace("-", "").substring(0, 20);
    }
}
