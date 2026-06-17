package com.robustrade.wallet_transfer_service.service;

import com.robustrade.wallet_transfer_service.dto.TransferRequest;
import com.robustrade.wallet_transfer_service.dto.TransferResponse;
import com.robustrade.wallet_transfer_service.entity.Transfer;
import com.robustrade.wallet_transfer_service.entity.Wallet;
import com.robustrade.wallet_transfer_service.enums.TransferStatus;
import com.robustrade.wallet_transfer_service.exception.InsufficientBalanceException;
import com.robustrade.wallet_transfer_service.repository.LedgerEntryRepository;
import com.robustrade.wallet_transfer_service.repository.TransferRepository;
import com.robustrade.wallet_transfer_service.repository.WalletRepository;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import com.robustrade.wallet_transfer_service.entity.LedgerEntry;
import com.robustrade.wallet_transfer_service.enums.LedgerType;

import java.time.LocalDateTime;
import java.util.Optional;

@Service
@RequiredArgsConstructor
public class TransferService {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;

    @Transactional
    public TransferResponse transferMoney(TransferRequest request) {

        Optional<Transfer> existingTransfer =
                transferRepository.findByIdempotencyKey(
                        request.getIdempotencyKey());

        if (existingTransfer.isPresent()) {

            Transfer transfer = existingTransfer.get();

            return TransferResponse.builder()
                    .transferId(transfer.getId())
                    .status(transfer.getStatus())
                    .amount(transfer.getAmount())
                    .build();
        }

        Wallet sourceWallet =
                walletRepository.findById(
                        request.getFromWalletId())
                        .orElseThrow(() ->
                                new RuntimeException("Source wallet not found"));

        Wallet destinationWallet =
                walletRepository.findById(
                        request.getToWalletId())
                        .orElseThrow(() ->
                                new RuntimeException("Destination wallet not found"));

        if (sourceWallet.getBalance() < request.getAmount()) {
            throw new InsufficientBalanceException(
                    "Insufficient balance");
        }

        Transfer transfer = Transfer.builder()
                .idempotencyKey(request.getIdempotencyKey())
                .fromWalletId(request.getFromWalletId())
                .toWalletId(request.getToWalletId())
                .amount(request.getAmount())
                .status(TransferStatus.PENDING)
                .createdAt(LocalDateTime.now())
                .build();

        transfer = transferRepository.save(transfer);

        sourceWallet.setBalance(
                sourceWallet.getBalance() - request.getAmount());

        destinationWallet.setBalance(
                destinationWallet.getBalance() + request.getAmount());

        walletRepository.save(sourceWallet);
        walletRepository.save(destinationWallet);

        LedgerEntry debitEntry =
        LedgerEntry.builder()
                .transferId(transfer.getId())
                .walletId(sourceWallet.getId())
                .type(LedgerType.DEBIT)
                .amount(request.getAmount())
                .build();

LedgerEntry creditEntry =
        LedgerEntry.builder()
                .transferId(transfer.getId())
                .walletId(destinationWallet.getId())
                .type(LedgerType.CREDIT)
                .amount(request.getAmount())
                .build();

           ledgerEntryRepository.save(debitEntry);
           ledgerEntryRepository.save(creditEntry);

        transfer.setStatus(TransferStatus.PROCESSED);

        transferRepository.save(transfer);

        return TransferResponse.builder()
                .transferId(transfer.getId())
                .status(transfer.getStatus())
                .amount(transfer.getAmount())
                .build();
    }
}