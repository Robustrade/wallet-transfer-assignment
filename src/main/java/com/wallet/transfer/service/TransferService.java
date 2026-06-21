package com.wallet.transfer.service;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.transfer.domain.IdempotencyRecord;
import com.wallet.transfer.domain.LedgerEntry;
import com.wallet.transfer.domain.LedgerEntryType;
import com.wallet.transfer.domain.Transfer;
import com.wallet.transfer.domain.TransferState;
import com.wallet.transfer.domain.Wallet;
import com.wallet.transfer.dto.CreateTransferRequest;
import com.wallet.transfer.dto.TransferResponse;
import com.wallet.transfer.exception.IdempotencyConflictException;
import com.wallet.transfer.exception.InsufficientFundsException;
import com.wallet.transfer.exception.WalletNotFoundException;
import com.wallet.transfer.repository.LedgerEntryRepository;
import com.wallet.transfer.repository.TransferRepository;
import com.wallet.transfer.repository.WalletRepository;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import java.time.LocalDateTime;
import java.util.Optional;
import java.util.UUID;

@Service
public class TransferService {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyService idempotencyService;
    private final ObjectMapper objectMapper;

    public TransferService(WalletRepository walletRepository,
                           TransferRepository transferRepository,
                           LedgerEntryRepository ledgerEntryRepository,
                           IdempotencyService idempotencyService,
                           ObjectMapper objectMapper) {
        this.walletRepository = walletRepository;
        this.transferRepository = transferRepository;
        this.ledgerEntryRepository = ledgerEntryRepository;
        this.idempotencyService = idempotencyService;
        this.objectMapper = objectMapper;
    }

    @Transactional(noRollbackFor = InsufficientFundsException.class)
    public TransferResponse createTransfer(CreateTransferRequest request) {
        String key = request.idempotencyKey();
        boolean hasIdempotencyKey = key != null && !key.trim().isEmpty();

        if (hasIdempotencyKey) {
            String hash = calculateRequestHash(request);
            Optional<IdempotencyRecord> existing = idempotencyService.claimKey(key, hash);
            if (existing.isPresent()) {
                IdempotencyRecord record = existing.get();
                if (!record.getRequestHash().equals(hash)) {
                    throw new IdempotencyConflictException("Idempotency key has been used with different request parameters");
                }
                return replayFromTransfer(key);
            }
        }

        String fromId = request.fromWalletId();
        String toId = request.toWalletId();
        if (fromId.equals(toId)) {
            throw new IllegalArgumentException("Cannot transfer to the same wallet");
        }

        Wallet firstWallet;
        Wallet secondWallet;
        if (fromId.compareTo(toId) < 0) {
            firstWallet = walletRepository.findByIdForUpdate(fromId)
                    .orElseThrow(() -> new WalletNotFoundException("Source wallet not found: " + fromId));
            secondWallet = walletRepository.findByIdForUpdate(toId)
                    .orElseThrow(() -> new WalletNotFoundException("Destination wallet not found: " + toId));
        } else {
            secondWallet = walletRepository.findByIdForUpdate(toId)
                    .orElseThrow(() -> new WalletNotFoundException("Destination wallet not found: " + toId));
            firstWallet = walletRepository.findByIdForUpdate(fromId)
                    .orElseThrow(() -> new WalletNotFoundException("Source wallet not found: " + fromId));
        }

        Wallet fromWallet = fromId.equals(firstWallet.getId()) ? firstWallet : secondWallet;
        Wallet toWallet = toId.equals(firstWallet.getId()) ? firstWallet : secondWallet;

        String transferId = "T-" + UUID.randomUUID().toString();
        Transfer transfer = new Transfer(
                transferId,
                fromId,
                toId,
                request.amount(),
                TransferState.PENDING,
                key,
                LocalDateTime.now()
        );
        transferRepository.save(transfer);

        if (fromWallet.getBalance() < request.amount()) {
            transfer.setState(TransferState.FAILED);
            transferRepository.save(transfer);
            throw new InsufficientFundsException("Insufficient funds in wallet: " + fromId);
        }

        fromWallet.setBalance(fromWallet.getBalance() - request.amount());
        toWallet.setBalance(toWallet.getBalance() + request.amount());
        walletRepository.save(fromWallet);
        walletRepository.save(toWallet);

        LedgerEntry debitEntry = new LedgerEntry(
                "LE-" + UUID.randomUUID().toString(),
                fromId,
                transferId,
                LedgerEntryType.DEBIT,
                request.amount(),
                LocalDateTime.now()
        );
        LedgerEntry creditEntry = new LedgerEntry(
                "LE-" + UUID.randomUUID().toString(),
                toId,
                transferId,
                LedgerEntryType.CREDIT,
                request.amount(),
                LocalDateTime.now()
        );
        ledgerEntryRepository.save(debitEntry);
        ledgerEntryRepository.save(creditEntry);

        transfer.setState(TransferState.PROCESSED);
        transferRepository.save(transfer);

        return new TransferResponse(
                transferId,
                fromId,
                toId,
                request.amount(),
                TransferState.PROCESSED,
                transfer.getCreatedAt()
        );
    }

    private TransferResponse replayFromTransfer(String idempotencyKey) {
        Optional<Transfer> found = transferRepository.findByIdempotencyKey(idempotencyKey);
        if (found.isEmpty() || found.get().getState() == TransferState.PENDING) {
            throw new IdempotencyConflictException("Request with this idempotency key is already in progress");
        }
        Transfer t = found.get();
        if (t.getState() == TransferState.FAILED) {
            throw new InsufficientFundsException("Insufficient funds in wallet: " + t.getFromWalletId());
        }
        return new TransferResponse(t.getId(), t.getFromWalletId(), t.getToWalletId(), t.getAmount(), t.getState(), t.getCreatedAt());
    }

    private String calculateRequestHash(CreateTransferRequest request) {
        try {
            // Hash a stable JSON serialization to avoid delimiter ambiguities.
            byte[] payload = objectMapper.writeValueAsBytes(
                    new CreateTransferRequest(null, request.fromWalletId(), request.toWalletId(), request.amount())
            );
            byte[] digest = java.security.MessageDigest.getInstance("SHA-256").digest(payload);
            return java.util.HexFormat.of().formatHex(digest);
        } catch (Exception e) {
            throw new RuntimeException("Failed to compute idempotency request hash", e);
        }
    }
}
