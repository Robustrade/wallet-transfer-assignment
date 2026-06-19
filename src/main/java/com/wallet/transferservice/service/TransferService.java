package com.wallet.transferservice.service;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.transferservice.dto.TransferRequest;
import com.wallet.transferservice.dto.TransferResponse;
import com.wallet.transferservice.domain.*;
import com.wallet.transferservice.domain.enums.LedgerEntryType;
import com.wallet.transferservice.domain.enums.WalletStatus;
import com.wallet.transferservice.exception.*;
import com.wallet.transferservice.repository.*;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.math.BigDecimal;
import java.util.UUID;

@Service
public class TransferService {

    private static final Logger log = LoggerFactory.getLogger(TransferService.class);

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final ObjectMapper objectMapper;

    public TransferService(WalletRepository walletRepository,
                           TransferRepository transferRepository,
                           LedgerEntryRepository ledgerEntryRepository,
                           IdempotencyRecordRepository idempotencyRecordRepository,
                           ObjectMapper objectMapper) {
        this.walletRepository = walletRepository;
        this.transferRepository = transferRepository;
        this.ledgerEntryRepository = ledgerEntryRepository;
        this.idempotencyRecordRepository = idempotencyRecordRepository;
        this.objectMapper = objectMapper;
    }

    /**
     * Executes a wallet-to-wallet transfer with full idempotency, pessimistic locking,
     * and double-entry ledger accounting.
     *
     * @return a result containing the response and whether it's a replay (200) or new (201)
     */
    @Transactional
    public TransferResult executeTransfer(TransferRequest request) {
        UUID idempotencyKey = UUID.fromString(request.idempotencyKey());
        UUID fromWalletId = UUID.fromString(request.fromWalletId());
        UUID toWalletId = UUID.fromString(request.toWalletId());
        BigDecimal amount = new BigDecimal(request.amount());

        // ── Validate: no self-transfer ────────────────────────
        if (fromWalletId.equals(toWalletId)) {
            throw new SelfTransferException();
        }

        // ── Validate: amount must be positive ─────────────────
        if (amount.compareTo(BigDecimal.ZERO) <= 0) {
            throw new IllegalArgumentException("Amount must be positive.");
        }

        // ── Step 1: Idempotency check ─────────────────────────
        var existingRecord = idempotencyRecordRepository.findById(idempotencyKey).orElse(null);

        if (existingRecord != null) {
            if (existingRecord.isCompleted()) {
                // The original request finished. Check parameter match.
                if (!existingRecord.parametersMatch(fromWalletId, toWalletId, amount)) {
                    throw new IdempotencyKeyConflictException(request.idempotencyKey());
                }
                // Replay: return stored response
                TransferResponse storedResponse = deserializeResponse(existingRecord.getResponseBody());
                return new TransferResult(storedResponse, true);
            } else {
                // Request is in-flight (response_body is null). Another transaction is processing.
                throw new TransferInFlightException(request.idempotencyKey());
            }
        }

        // ── Step 2: Insert idempotency record (claim the key) ─
        IdempotencyRecord idempotencyRecord = new IdempotencyRecord(
                idempotencyKey, fromWalletId, toWalletId, amount
        );
        try {
            idempotencyRecordRepository.saveAndFlush(idempotencyRecord);
        } catch (DataIntegrityViolationException e) {
            // Another concurrent request inserted the same key between our SELECT and INSERT.
            // Re-read and either replay or report in-flight.
            log.info("Idempotency key race detected for key={}", idempotencyKey);
            var raceRecord = idempotencyRecordRepository.findById(idempotencyKey).orElse(null);
            if (raceRecord != null && raceRecord.isCompleted()) {
                if (!raceRecord.parametersMatch(fromWalletId, toWalletId, amount)) {
                    throw new IdempotencyKeyConflictException(request.idempotencyKey());
                }
                return new TransferResult(deserializeResponse(raceRecord.getResponseBody()), true);
            }
            throw new TransferInFlightException(request.idempotencyKey());
        }

        // ── Step 3: Acquire locks in deterministic order ──────
        // Sort wallet IDs ascending to prevent deadlocks.
        UUID lockFirst, lockSecond;
        if (fromWalletId.compareTo(toWalletId) < 0) {
            lockFirst = fromWalletId;
            lockSecond = toWalletId;
        } else {
            lockFirst = toWalletId;
            lockSecond = fromWalletId;
        }

        Wallet wallet1 = walletRepository.findByIdForUpdate(lockFirst)
                .orElseThrow(() -> {
                    cleanupIdempotencyRecord(idempotencyKey);
                    return new WalletNotFoundException(lockFirst);
                });

        Wallet wallet2 = walletRepository.findByIdForUpdate(lockSecond)
                .orElseThrow(() -> {
                    cleanupIdempotencyRecord(idempotencyKey);
                    return new WalletNotFoundException(lockSecond);
                });

        // Map back to source/destination based on original IDs
        Wallet sourceWallet = wallet1.getId().equals(fromWalletId) ? wallet1 : wallet2;
        Wallet destWallet = wallet1.getId().equals(toWalletId) ? wallet1 : wallet2;

        // ── Step 4: Validate wallet status ────────────────────
        if (sourceWallet.getStatus() != WalletStatus.ACTIVE) {
            cleanupIdempotencyRecord(idempotencyKey);
            throw new WalletInactiveException(fromWalletId);
        }
        // Destination can be ACTIVE or FROZEN (can receive but not send)
        if (destWallet.getStatus() == WalletStatus.CLOSED) {
            cleanupIdempotencyRecord(idempotencyKey);
            throw new WalletInactiveException(toWalletId);
        }

        // ── Step 5: Debit source (enforces sufficient funds), credit destination ──
        try {
            sourceWallet.debit(amount);
        } catch (InsufficientFundsException e) {
            cleanupIdempotencyRecord(idempotencyKey);
            throw e;
        }
        
        destWallet.credit(amount);

        walletRepository.save(sourceWallet);
        walletRepository.save(destWallet);

        // ── Step 7: Insert transfer record ────────────────────
        Transfer transfer = new Transfer(idempotencyKey, fromWalletId, toWalletId, amount);
        transfer = transferRepository.save(transfer);

        // ── Step 8: Insert ledger entries ─────────────────────
        LedgerEntry debitEntry = new LedgerEntry(
                transfer.getId(), fromWalletId, LedgerEntryType.DEBIT,
                amount.negate(), sourceWallet.getBalance()
        );
        LedgerEntry creditEntry = new LedgerEntry(
                transfer.getId(), toWalletId, LedgerEntryType.CREDIT,
                amount, destWallet.getBalance()
        );
        ledgerEntryRepository.save(debitEntry);
        ledgerEntryRepository.save(creditEntry);

        // ── Step 9: Build response and update idempotency record ─
        TransferResponse response = new TransferResponse(
                transfer.getId().toString(),
                idempotencyKey.toString(),
                fromWalletId.toString(),
                toWalletId.toString(),
                amount.toPlainString(),
                transfer.getStatus().name(),
                transfer.getCreatedAt()
        );

        idempotencyRecord.setTransferId(transfer.getId());
        idempotencyRecord.setHttpStatusCode((short) 201);
        idempotencyRecord.setResponseBody(serializeResponse(response));
        idempotencyRecordRepository.save(idempotencyRecord);

        log.info("Transfer completed: id={}, from={}, to={}, amount={}",
                transfer.getId(), fromWalletId, toWalletId, amount.toPlainString());

        return new TransferResult(response, false);
    }

    // ── Helpers ───────────────────────────────────────────────

    private void cleanupIdempotencyRecord(UUID idempotencyKey) {
        try {
            idempotencyRecordRepository.deleteById(idempotencyKey);
            idempotencyRecordRepository.flush();
        } catch (Exception e) {
            log.warn("Failed to cleanup idempotency record for key={}: {}", idempotencyKey, e.getMessage());
        }
    }

    private String serializeResponse(TransferResponse response) {
        try {
            return objectMapper.writeValueAsString(response);
        } catch (JsonProcessingException e) {
            throw new RuntimeException("Failed to serialize transfer response", e);
        }
    }

    private TransferResponse deserializeResponse(String json) {
        try {
            return objectMapper.readValue(json, TransferResponse.class);
        } catch (JsonProcessingException e) {
            throw new RuntimeException("Failed to deserialize stored transfer response", e);
        }
    }

    // ── Result wrapper ────────────────────────────────────────

    /**
     * Wraps the response with metadata about whether it's a replay (idempotent).
     *
     * @param response the transfer response body
     * @param replay   true if this is an idempotent replay (return 200), false if new (return 201)
     */
    public record TransferResult(TransferResponse response, boolean replay) {
    }
}
