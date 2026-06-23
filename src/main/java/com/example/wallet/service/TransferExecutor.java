package com.example.wallet.service;

import com.example.wallet.domain.LedgerEntry;
import com.example.wallet.domain.LedgerEntryType;
import com.example.wallet.domain.Transfer;
import com.example.wallet.domain.Wallet;
import com.example.wallet.repository.LedgerEntryRepository;
import com.example.wallet.repository.TransferRepository;
import com.example.wallet.repository.WalletRepository;
import com.example.wallet.web.dto.TransferRequest;
import com.example.wallet.web.dto.TransferResponse;
import java.util.Optional;
import java.util.UUID;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Component;
import org.springframework.transaction.annotation.Transactional;

@Component
public class TransferExecutor {

    private final TransferRepository transferRepository;
    private final WalletRepository walletRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyService idempotencyService;

    public TransferExecutor(
            TransferRepository transferRepository,
            WalletRepository walletRepository,
            LedgerEntryRepository ledgerEntryRepository,
            IdempotencyService idempotencyService) {
        this.transferRepository = transferRepository;
        this.walletRepository = walletRepository;
        this.ledgerEntryRepository = ledgerEntryRepository;
        this.idempotencyService = idempotencyService;
    }

    @Transactional
    public TransferExecutionResult execute(TransferRequest request) {
        Transfer transfer = new Transfer(
                UUID.randomUUID().toString(),
                request.fromWalletId(),
                request.toWalletId(),
                request.amount(),
                request.idempotencyKey());
        transferRepository.save(transfer);

        if (request.fromWalletId().equals(request.toWalletId())) {
            return failTransfer(transfer, HttpStatus.BAD_REQUEST.value(), "source and destination wallets must differ");
        }

        LockedWallets lockedWallets = lockWalletsInOrder(request.fromWalletId(), request.toWalletId());

        if (!lockedWallets.bothPresent()) {
            String message = lockedWallets.sourceWallet() == null
                    ? "source wallet not found: " + request.fromWalletId()
                    : "destination wallet not found: " + request.toWalletId();
            return failTransfer(transfer, HttpStatus.NOT_FOUND.value(), message);
        }

        Wallet sourceWallet = lockedWallets.sourceWallet();
        Wallet destinationWallet = lockedWallets.destinationWallet();

        if (sourceWallet.getBalance().compareTo(request.amount()) < 0) {
            return failTransfer(transfer, HttpStatus.UNPROCESSABLE_ENTITY.value(), "insufficient funds");
        }

        sourceWallet.debit(request.amount());
        destinationWallet.credit(request.amount());
        walletRepository.save(sourceWallet);
        walletRepository.save(destinationWallet);

        ledgerEntryRepository.save(new LedgerEntry(
                sourceWallet.getId(), transfer.getId(), LedgerEntryType.DEBIT, request.amount()));
        ledgerEntryRepository.save(new LedgerEntry(
                destinationWallet.getId(), transfer.getId(), LedgerEntryType.CREDIT, request.amount()));

        transfer.markProcessed();
        transferRepository.save(transfer);

        TransferExecutionResult result =
                new TransferExecutionResult(HttpStatus.CREATED.value(), toResponse(transfer));
        idempotencyService.storeResult(request.idempotencyKey(), result);
        return result;
    }

    private LockedWallets lockWalletsInOrder(String fromWalletId, String toWalletId) {
        String firstId = fromWalletId.compareTo(toWalletId) <= 0 ? fromWalletId : toWalletId;
        String secondId = fromWalletId.compareTo(toWalletId) <= 0 ? toWalletId : fromWalletId;

        Optional<Wallet> firstWallet = walletRepository.findByIdForUpdate(firstId);
        Optional<Wallet> secondWallet = walletRepository.findByIdForUpdate(secondId);

        Wallet sourceWallet = fromWalletId.equals(firstId) ? firstWallet.orElse(null) : secondWallet.orElse(null);
        Wallet destinationWallet =
                toWalletId.equals(firstId) ? firstWallet.orElse(null) : secondWallet.orElse(null);

        return new LockedWallets(sourceWallet, destinationWallet);
    }

    private TransferExecutionResult failTransfer(Transfer transfer, int statusCode, String message) {
        transfer.markFailed(message);
        transferRepository.save(transfer);

        TransferExecutionResult result =
                new TransferExecutionResult(statusCode, toResponse(transfer));
        idempotencyService.storeResult(transfer.getIdempotencyKey(), result);
        return result;
    }

    private TransferResponse toResponse(Transfer transfer) {
        return new TransferResponse(
                transfer.getId(),
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount(),
                transfer.getStatus(),
                transfer.getErrorMessage());
    }

    private record LockedWallets(Wallet sourceWallet, Wallet destinationWallet) {

        boolean bothPresent() {
            return sourceWallet != null && destinationWallet != null;
        }
    }
}
