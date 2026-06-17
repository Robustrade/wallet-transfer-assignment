package com.siddharth.wallet.service;

import com.siddharth.wallet.dto.CreateTransferRequest;
import com.siddharth.wallet.dto.TransferResponse;
import com.siddharth.wallet.exceptions.DuplicateTransferException;
import com.siddharth.wallet.exceptions.InsufficientFundsException;
import com.siddharth.wallet.exceptions.InvalidTransferException;
import com.siddharth.wallet.exceptions.WalletNotFoundException;
import com.siddharth.wallet.model.*;
import com.siddharth.wallet.repository.LedgerEntryRepository;
import com.siddharth.wallet.repository.TransferRepository;
import com.siddharth.wallet.repository.WalletRepository;
import jakarta.transaction.Transactional;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;

import java.math.BigDecimal;
import java.util.UUID;

@Service
@RequiredArgsConstructor
@Transactional
public class TransferServiceImpl implements TransferService {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;

    @Override
    public TransferResponse transfer(
            CreateTransferRequest request) {

        validateRequest(request);

        Transfer existingTransfer =
                findExistingTransfer(
                        request.idempotencyKey());

        if (existingTransfer != null) {
            throw new DuplicateTransferException(request.idempotencyKey());
        }

        return executeTransfer(request);
    }

    private TransferResponse executeTransfer(
            CreateTransferRequest request) {

        Wallet sourceWallet =
                getWalletForUpdate(
                        request.fromWalletId());

        Wallet destinationWallet =
                getWalletForUpdate(
                        request.toWalletId());

        validateSufficientFunds(
                sourceWallet,
                request.amount());

        Transfer transfer =
                createPendingTransfer(request);

        updateBalances(
                sourceWallet,
                destinationWallet,
                request.amount());

        createLedgerEntries(
                transfer,
                sourceWallet,
                destinationWallet,
                request.amount());

        markTransferProcessed(transfer);

        return map(transfer);
    }

    private void validateRequest(CreateTransferRequest request) {
        if (request.fromWalletId()
                .equals(request.toWalletId())) {

            throw new InvalidTransferException(
                    "Source and destination wallet cannot be same");
        }
    }

    private Transfer findExistingTransfer(
            String idempotencyKey) {

        return transferRepository
                .findByIdempotencyKey(idempotencyKey)
                .orElse(null);
    }

    private Wallet getWalletForUpdate(
            UUID walletId) {

        return walletRepository
                .findByIdForUpdate(walletId)
                .orElseThrow(() ->
                        new WalletNotFoundException(walletId));
    }

    private void validateSufficientFunds(
            Wallet wallet,
            BigDecimal amount) {

        if (wallet.getBalance()
                .compareTo(amount) < 0) {

            throw new InsufficientFundsException(
                    wallet.getId());
        }
    }

    private Transfer createPendingTransfer(
            CreateTransferRequest request) {

        Transfer transfer = new Transfer();

        transfer.setId(UUID.randomUUID());
        transfer.setIdempotencyKey(
                request.idempotencyKey());
        transfer.setFromWalletId(
                request.fromWalletId());
        transfer.setToWalletId(
                request.toWalletId());
        transfer.setAmount(
                request.amount());
        transfer.setStatus(
                TransferStatus.PENDING);

        return transferRepository.save(transfer);
    }

    private void updateBalances(
            Wallet sourceWallet,
            Wallet destinationWallet,
            BigDecimal amount) {

        sourceWallet.setBalance(
                sourceWallet.getBalance()
                        .subtract(amount));

        destinationWallet.setBalance(
                destinationWallet.getBalance()
                        .add(amount));

        walletRepository.save(sourceWallet);
        walletRepository.save(destinationWallet);
    }

    private void createLedgerEntries(
            Transfer transfer,
            Wallet sourceWallet,
            Wallet destinationWallet,
            BigDecimal amount) {

        ledgerEntryRepository.save(
                buildDebitEntry(
                        transfer.getId(),
                        sourceWallet.getId(),
                        amount));

        ledgerEntryRepository.save(
                buildCreditEntry(
                        transfer.getId(),
                        destinationWallet.getId(),
                        amount));
    }

    private LedgerEntry buildDebitEntry(
            UUID transferId,
            UUID walletId,
            BigDecimal amount) {

        LedgerEntry entry = new LedgerEntry();

        entry.setId(UUID.randomUUID());
        entry.setTransferId(transferId);
        entry.setWalletId(walletId);
        entry.setType(EntryType.DEBIT);
        entry.setAmount(amount);

        return entry;
    }

    private LedgerEntry buildCreditEntry(
            UUID transferId,
            UUID walletId,
            BigDecimal amount) {

        LedgerEntry entry = new LedgerEntry();

        entry.setId(UUID.randomUUID());
        entry.setTransferId(transferId);
        entry.setWalletId(walletId);
        entry.setType(EntryType.CREDIT);
        entry.setAmount(amount);

        return entry;
    }

    private void markTransferProcessed(
            Transfer transfer) {

        transfer.setStatus(
                TransferStatus.PROCESSED);

        transferRepository.save(transfer);
    }

    private TransferResponse map(
            Transfer transfer) {

        return new TransferResponse(
                transfer.getId(),
                transfer.getStatus(),
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount()
        );
    }
}
