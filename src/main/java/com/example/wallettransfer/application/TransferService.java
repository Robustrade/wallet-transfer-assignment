package com.example.wallettransfer.application;

import com.example.wallettransfer.domain.LedgerEntryType;
import com.example.wallettransfer.domain.Transfer;
import com.example.wallettransfer.domain.Wallet;
import com.example.wallettransfer.persistence.TransferRepository;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.util.List;
import java.util.Map;
import java.util.UUID;
import java.util.function.Function;
import java.util.stream.Collectors;

@Service
public class TransferService {

    private final TransferRepository transferRepository;
    private final RequestHasher requestHasher;

    public TransferService(TransferRepository transferRepository, RequestHasher requestHasher) {
        this.transferRepository = transferRepository;
        this.requestHasher = requestHasher;
    }

    @Transactional
    public TransferResult createTransfer(CreateTransferCommand rawCommand) {
        CreateTransferCommand command = validate(rawCommand).normalized();
        String requestHash = requestHasher.hash(command);
        UUID transferId = UUID.randomUUID();

        Transfer inserted;
        try {
            inserted = transferRepository.insertPendingTransferIfAbsent(transferId, command, requestHash)
                    .orElse(null);
        } catch (DataIntegrityViolationException ex) {
            throw new WalletNotFoundException("One or both wallets do not exist");
        }

        if (inserted == null) {
            Transfer existing = transferRepository.findByIdempotencyKey(command.idempotencyKey())
                    .orElseThrow(() -> new IllegalStateException("Idempotency conflict was not readable"));
            if (!existing.requestHash().equals(requestHash)) {
                throw new IdempotencyKeyReuseException("Idempotency key was already used for a different request");
            }
            return toResult(existing);
        }

        List<Wallet> lockedWallets = transferRepository.lockWallets(command.fromWalletId(), command.toWalletId());
        Map<String, Wallet> walletsById = lockedWallets.stream()
                .collect(Collectors.toMap(Wallet::id, Function.identity()));

        Wallet fromWallet = walletsById.get(command.fromWalletId());
        Wallet toWallet = walletsById.get(command.toWalletId());
        if (fromWallet == null || toWallet == null) {
            throw new WalletNotFoundException("One or both wallets do not exist");
        }

        if (fromWallet.balance() < command.amount()) {
            Transfer failed = transferRepository.markFailed(
                    transferId,
                    "INSUFFICIENT_FUNDS");
            return toResult(failed);
        }

        transferRepository.updateWalletBalance(command.fromWalletId(), fromWallet.balance() - command.amount());
        transferRepository.updateWalletBalance(command.toWalletId(), toWallet.balance() + command.amount());
        transferRepository.insertLedgerEntry(transferId, command.fromWalletId(), LedgerEntryType.DEBIT, command.amount());
        transferRepository.insertLedgerEntry(transferId, command.toWalletId(), LedgerEntryType.CREDIT, command.amount());

        Transfer processed = transferRepository.markProcessed(transferId);
        return toResult(processed);
    }

    private CreateTransferCommand validate(CreateTransferCommand command) {
        if (command == null) {
            throw new InvalidTransferRequestException("Request body is required");
        }

        CreateTransferCommand normalized = command.normalized();
        if (isBlank(normalized.idempotencyKey())) {
            throw new InvalidTransferRequestException("idempotencyKey is required");
        }
        if (isBlank(normalized.fromWalletId())) {
            throw new InvalidTransferRequestException("fromWalletId is required");
        }
        if (isBlank(normalized.toWalletId())) {
            throw new InvalidTransferRequestException("toWalletId is required");
        }
        if (normalized.fromWalletId().equals(normalized.toWalletId())) {
            throw new InvalidTransferRequestException("fromWalletId and toWalletId must be different");
        }
        if (normalized.amount() <= 0) {
            throw new InvalidTransferRequestException("amount must be positive");
        }
        return normalized;
    }

    private boolean isBlank(String value) {
        return value == null || value.isBlank();
    }

    private TransferResult toResult(Transfer transfer) {
        return new TransferResult(
                transfer.id(),
                transfer.state(),
                transfer.fromWalletId(),
                transfer.toWalletId(),
                transfer.amount(),
                transfer.failureReason());
    }
}
