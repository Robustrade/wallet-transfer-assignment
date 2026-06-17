package com.assignment.wallet_transfer.service.impl;

import com.assignment.wallet_transfer.dtos.TransferHistoryResponse;
import com.assignment.wallet_transfer.dtos.TransferRequest;
import com.assignment.wallet_transfer.dtos.TransferResponse;
import com.assignment.wallet_transfer.entity.LedgerEntry;
import com.assignment.wallet_transfer.entity.Transfer;
import com.assignment.wallet_transfer.entity.Wallet;
import com.assignment.wallet_transfer.enums.EntryType;
import com.assignment.wallet_transfer.enums.TransferStatus;
import com.assignment.wallet_transfer.exception.InsufficientBalanceException;
import com.assignment.wallet_transfer.exception.WalletNotFoundException;
import com.assignment.wallet_transfer.repository.LedgerRepository;
import com.assignment.wallet_transfer.repository.TransferRepository;
import com.assignment.wallet_transfer.repository.WalletRepository;
import com.assignment.wallet_transfer.service.TransferService;
import jakarta.persistence.EntityNotFoundException;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.time.LocalDateTime;
import java.util.List;
import java.util.UUID;

@Service
@RequiredArgsConstructor
@Transactional
public class TransferServiceImpl implements TransferService {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerRepository ledgerRepository;

    @Override
    public TransferResponse transfer(
            TransferRequest request,
            String idempotencyKey) {

        var existingTransfer =
                transferRepository.findByIdempotencyKey(idempotencyKey);

        if (existingTransfer.isPresent()) {

            Transfer transfer = existingTransfer.get();


            return new TransferResponse(
                    transfer.getId(),
                    transfer.getStatus(),
                    transfer.getAmount()
            );
        }

        Wallet sourceWallet = walletRepository.findById(
                        request.sourceWalletId())
                .orElseThrow(() ->
                        new WalletNotFoundException(
                                "Source wallet not found"));

        Wallet destinationWallet = walletRepository.findById(
                        request.destinationWalletId())
                .orElseThrow(() ->
                        new WalletNotFoundException(
                                "Destination wallet not found"));

        if (sourceWallet.getId().equals(destinationWallet.getId())) {
            throw new IllegalArgumentException(
                    "Source and destination wallets cannot be the same");
        }

        if (sourceWallet.getBalance().compareTo(request.amount()) < 0) {
            throw new InsufficientBalanceException(
                    "Insufficient balance");
        }

        Transfer transfer = Transfer.builder()
                .sourceWalletId(sourceWallet.getId())
                .destinationWalletId(destinationWallet.getId())
                .amount(request.amount())
                .status(TransferStatus.PENDING)
                .idempotencyKey(idempotencyKey)
                .createdAt(LocalDateTime.now())
                .build();

        transfer = transferRepository.save(transfer);

        try {

            sourceWallet.setBalance(
                    sourceWallet.getBalance()
                            .subtract(request.amount()));

            destinationWallet.setBalance(
                    destinationWallet.getBalance()
                            .add(request.amount()));

            walletRepository.save(sourceWallet);
            walletRepository.save(destinationWallet);

            LedgerEntry debit = LedgerEntry.builder()
                    .transferId(transfer.getId())
                    .walletId(sourceWallet.getId())
                    .entryType(EntryType.DEBIT)
                    .amount(request.amount())
                    .createdAt(LocalDateTime.now())
                    .build();

            LedgerEntry credit = LedgerEntry.builder()
                    .transferId(transfer.getId())
                    .walletId(destinationWallet.getId())
                    .entryType(EntryType.CREDIT)
                    .amount(request.amount())
                    .createdAt(LocalDateTime.now())
                    .build();

            ledgerRepository.save(debit);
            ledgerRepository.save(credit);

            transfer.setStatus(TransferStatus.PROCESSED);
            transferRepository.save(transfer);

            return new TransferResponse(
                    transfer.getId(),
                    transfer.getStatus(),
                    transfer.getAmount()
            );

        } catch (Exception ex) {

            transfer.setStatus(TransferStatus.FAILED);
            transferRepository.save(transfer);

            throw ex;
        }
    }

    @Override
    @Transactional(readOnly = true)
    public List<TransferHistoryResponse> getWalletTransfers(
            UUID walletId) {

        walletRepository.findById(walletId)
                .orElseThrow(() ->
                        new WalletNotFoundException(
                                "Wallet not found"));

        return transferRepository
                .findBySourceWalletIdOrDestinationWalletId(
                        walletId,
                        walletId
                )
                .stream()
                .map(transfer ->
                        new TransferHistoryResponse(
                                transfer.getId(),
                                transfer.getSourceWalletId(),
                                transfer.getDestinationWalletId(),
                                transfer.getAmount(),
                                transfer.getStatus(),
                                transfer.getCreatedAt()
                        )
                )
                .toList();
    }
}