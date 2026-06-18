package com.wallet.service;

import java.math.BigDecimal;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;
import java.util.Optional;
import java.util.UUID;

import org.springframework.dao.DuplicateKeyException;
import org.springframework.stereotype.Service;

import com.wallet.domain.Money;
import com.wallet.domain.Transfer;
import com.wallet.exception.IdempotencyConflictException;
import com.wallet.exception.InvalidTransferRequestException;
import com.wallet.exception.TransferNotFoundException;
import com.wallet.repository.TransferRepository;

@Service
public class TransferService {

    private final TransferRepository transferRepository;
    private final TransferExecutor transferExecutor;

    public TransferService(TransferRepository transferRepository, TransferExecutor transferExecutor) {
        this.transferRepository = transferRepository;
        this.transferExecutor = transferExecutor;
    }

    public TransferResult transfer(String idempotencyKey, UUID fromWalletId, UUID toWalletId, BigDecimal amount) {
        if (idempotencyKey == null || idempotencyKey.isBlank()) {
            throw new InvalidTransferRequestException("idempotencyKey must not be blank");
        }
        if (fromWalletId == null || toWalletId == null) {
            throw new InvalidTransferRequestException("fromWalletId and toWalletId must not be null");
        }
        if (fromWalletId.equals(toWalletId)) {
            throw new InvalidTransferRequestException("fromWalletId and toWalletId must differ");
        }
        if (amount == null || amount.signum() <= 0) {
            throw new InvalidTransferRequestException("amount must be positive");
        }

        String fingerprint = fingerprint(fromWalletId, toWalletId, amount);

        Optional<Transfer> existing = transferRepository.findByIdempotencyKey(idempotencyKey);
        if (existing.isPresent()) {
            return replayOrConflict(existing.get(), fingerprint);
        }

        try {
            return transferExecutor.execute(idempotencyKey, fingerprint, fromWalletId, toWalletId, amount);
        } catch (DuplicateKeyException concurrentDuplicate) {
            // Another transaction committed a transfer under this idempotency key while we were
            // working; our own insert was rejected and that whole transaction rolled back
            // cleanly (including our wallet locks). A fresh lookup now finds the committed row.
            Transfer raced = transferRepository.findByIdempotencyKey(idempotencyKey)
                    .orElseThrow(() -> concurrentDuplicate);
            return replayOrConflict(raced, fingerprint);
        }
    }

    public Transfer getTransfer(UUID id) {
        return transferRepository.findById(id).orElseThrow(() -> new TransferNotFoundException(id));
    }

    private TransferResult replayOrConflict(Transfer existing, String fingerprint) {
        if (!existing.getRequestFingerprint().equals(fingerprint)) {
            throw new IdempotencyConflictException(existing.getIdempotencyKey());
        }
        return new TransferResult(existing, true);
    }

    /**
     * Canonical hash of the request payload, stored alongside the idempotency key so a key reused
     * with a different fromWalletId/toWalletId/amount is detected as a conflict rather than silently
     * replayed. Amount is normalized to the ledger's fixed scale so "100" and "100.00" fingerprint
     * identically.
     */
    private String fingerprint(UUID fromWalletId, UUID toWalletId, BigDecimal amount) {
        String canonical = fromWalletId + "|" + toWalletId + "|" + Money.normalize(amount).toPlainString();
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            return HexFormat.of().formatHex(digest.digest(canonical.getBytes(StandardCharsets.UTF_8)));
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException("SHA-256 not available", e);
        }
    }
}
