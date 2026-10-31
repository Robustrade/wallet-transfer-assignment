package com.wallet.service;

import com.wallet.domain.IdempotencyRecord;
import com.wallet.domain.LedgerEntry;
import com.wallet.domain.Transfer;
import com.wallet.domain.Wallet;
import com.wallet.exception.InsufficientBalanceException;
import com.wallet.repository.IdempotencyRecordRepository;
import com.wallet.repository.LedgerEntryRepository;
import com.wallet.repository.TransferRepository;
import com.wallet.repository.WalletRepository;
import com.wallet.dto.CreateTransferRequest;
import com.wallet.dto.TransferResponse;
import com.wallet.exception.IdempotencyKeyReusedException;
import com.wallet.exception.WalletNotFoundException;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.transaction.support.TransactionTemplate;

import java.math.BigDecimal;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.List;
import java.util.NoSuchElementException;
import java.util.Optional;

@Service
public class TransferService {

    private static final Logger log = LoggerFactory.getLogger(TransferService.class);

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final TransactionTemplate transactionTemplate;
    private final TransactionTemplate readOnlyTransactionTemplate;

    public TransferService(
            WalletRepository walletRepository,
            TransferRepository transferRepository,
            LedgerEntryRepository ledgerEntryRepository,
            IdempotencyRecordRepository idempotencyRecordRepository,
            PlatformTransactionManager transactionManager
    ) {
        this.walletRepository = walletRepository;
        this.transferRepository = transferRepository;
        this.ledgerEntryRepository = ledgerEntryRepository;
        this.idempotencyRecordRepository = idempotencyRecordRepository;
        this.transactionTemplate = new TransactionTemplate(transactionManager);
        this.readOnlyTransactionTemplate = new TransactionTemplate(transactionManager);
        this.readOnlyTransactionTemplate.setReadOnly(true);
    }

    public TransferResponse createTransfer(CreateTransferRequest request) {
        if (request.fromWalletId().equals(request.toWalletId())) {
            throw new IllegalArgumentException("fromWalletId and toWalletId must differ");
        }
        if (request.amount().compareTo(BigDecimal.ZERO) <= 0) {
            throw new IllegalArgumentException("amount must be positive");
        }

        String requestHash = hash(request);

        try {
            return transactionTemplate.execute(status -> executeTransfer(request, requestHash));
        } catch (DataIntegrityViolationException concurrentDuplicateKey) {
            log.info("idempotencyKey {} was claimed by a concurrent request; replaying its committed result",
                    request.idempotencyKey());
            return readOnlyTransactionTemplate.execute(status -> replayCommittedResult(request, requestHash));
        }
    }

//    Runs entirely in one transaction.
    private TransferResponse executeTransfer(CreateTransferRequest request, String requestHash) {
        Optional<IdempotencyRecord> existing = idempotencyRecordRepository.findByKeyForUpdate(request.idempotencyKey());
        if (existing.isPresent()) {
            return replayRecord(existing.get(), requestHash, request.idempotencyKey());
        }

        // Lock both wallets, always in the same global order, to avoid
        // ABBA deadlocks against a concurrent transfer in the opposite
        // direction between the same two wallets.
        String firstId = request.fromWalletId().compareTo(request.toWalletId()) <= 0
                ? request.fromWalletId() : request.toWalletId();
        String secondId = firstId.equals(request.fromWalletId()) ? request.toWalletId() : request.fromWalletId();

        Wallet firstLocked = walletRepository.findByIdForUpdate(firstId)
                .orElseThrow(() -> new WalletNotFoundException(firstId));
        Wallet secondLocked = walletRepository.findByIdForUpdate(secondId)
                .orElseThrow(() -> new WalletNotFoundException(secondId));

        Wallet fromWallet = firstId.equals(request.fromWalletId()) ? firstLocked : secondLocked;
        Wallet toWallet = firstId.equals(request.fromWalletId()) ? secondLocked : firstLocked;

        Transfer transfer = Transfer.createPending(
                request.idempotencyKey(), request.fromWalletId(), request.toWalletId(), request.amount());

        try {
            fromWallet.debit(request.amount());
            toWallet.credit(request.amount());
        } catch (InsufficientBalanceException ex) {
            transfer.markFailed(ex.getMessage());
            transferRepository.save(transfer);
            idempotencyRecordRepository.save(new IdempotencyRecord(request.idempotencyKey(), requestHash, transfer.getId()));
            log.info("Transfer {} failed: {}", transfer.getId(), ex.getMessage());
            return TransferResponse.from(transfer, false);
        }

        walletRepository.save(fromWallet);
        walletRepository.save(toWallet);

        transfer.markProcessed();
        transferRepository.save(transfer);

        ledgerEntryRepository.save(LedgerEntry.debit(transfer.getId(), fromWallet.getId(), request.amount()));
        ledgerEntryRepository.save(LedgerEntry.credit(transfer.getId(), toWallet.getId(), request.amount()));

        idempotencyRecordRepository.save(new IdempotencyRecord(request.idempotencyKey(), requestHash, transfer.getId()));

        log.info("Transfer {} processed: {} -> {} amount={}",
                transfer.getId(), fromWallet.getId(), toWallet.getId(), request.amount());

        return TransferResponse.from(transfer, false);
    }

    /** Runs in its own fresh, read-only transaction (see {@link #createTransfer}). */
    private TransferResponse replayCommittedResult(CreateTransferRequest request, String requestHash) {
        IdempotencyRecord record = idempotencyRecordRepository.findById(request.idempotencyKey())
                .orElseThrow(() -> new IllegalStateException(
                        "Expected a committed idempotency record for key " + request.idempotencyKey()
                                + " after a unique-constraint conflict, but found none"));
        return replayRecord(record, requestHash, request.idempotencyKey());
    }

    private TransferResponse replayRecord(IdempotencyRecord record, String requestHash, String idempotencyKey) {
        if (!record.getRequestHash().equals(requestHash)) {
            throw new IdempotencyKeyReusedException(idempotencyKey);
        }
        Transfer transfer = transferRepository.findById(record.getTransferId())
                .orElseThrow(() -> new IllegalStateException(
                        "idempotency_records references missing transfer " + record.getTransferId()));
        log.info("Replaying idempotent transfer result for key={} transferId={}", idempotencyKey, transfer.getId());
        return TransferResponse.from(transfer, true);
    }

    @Transactional(readOnly = true)
    public Transfer getTransfer(java.util.UUID transferId) {
        return transferRepository.findById(transferId)
                .orElseThrow(() -> new NoSuchElementException("Transfer not found: " + transferId));
    }

    @Transactional(readOnly = true)
    public Wallet getWallet(String walletId) {
        return walletRepository.findById(walletId)
                .orElseThrow(() -> new WalletNotFoundException(walletId));
    }

    @Transactional(readOnly = true)
    public List<LedgerEntry> getWalletLedger(String walletId) {
        return ledgerEntryRepository.findByWalletIdOrderByCreatedAtDesc(walletId);
    }

    private String hash(CreateTransferRequest request) {
        String raw = request.fromWalletId() + "|" + request.toWalletId() + "|" + request.amount().stripTrailingZeros();
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            byte[] bytes = digest.digest(raw.getBytes(StandardCharsets.UTF_8));
            StringBuilder sb = new StringBuilder();
            for (byte b : bytes) {
                sb.append(String.format("%02x", b));
            }
            return sb.toString();
        } catch (NoSuchAlgorithmException e) {
            // SHA-256 is guaranteed to be available on every standard JVM.
            throw new IllegalStateException(e);
        }
    }
}

