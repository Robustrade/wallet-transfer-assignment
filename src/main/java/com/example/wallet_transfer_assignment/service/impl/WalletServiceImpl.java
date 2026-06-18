package com.example.wallet_transfer_assignment.service.impl;

import com.example.wallet_transfer_assignment.dto.BalanceResponse;
import com.example.wallet_transfer_assignment.dto.CreateWalletRequest;
import com.example.wallet_transfer_assignment.dto.TransactionHistoryResponse;
import com.example.wallet_transfer_assignment.dto.WalletResponse;
import com.example.wallet_transfer_assignment.entity.EntryType;
import com.example.wallet_transfer_assignment.entity.LedgerEntry;
import com.example.wallet_transfer_assignment.entity.Wallet;
import com.example.wallet_transfer_assignment.exception.WalletNotFoundException;
import com.example.wallet_transfer_assignment.repository.LedgerEntryRepository;
import com.example.wallet_transfer_assignment.repository.WalletRepository;
import com.example.wallet_transfer_assignment.service.WalletService;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.util.List;
import java.util.UUID;

@Service
@RequiredArgsConstructor
public class WalletServiceImpl implements WalletService {

    private final WalletRepository walletRepository;
    private final LedgerEntryRepository ledgerEntryRepository;

    @Override
    public WalletResponse createWallet(
            CreateWalletRequest request) {

        if (request.initialBalance().signum() < 0) {
            throw new IllegalArgumentException(
                    "Initial balance cannot be negative"
            );
        }

        Wallet wallet = Wallet.builder()
                .ownerName(request.ownerName())
                .balance(request.initialBalance())
                .build();

        walletRepository.save(wallet);

        return new WalletResponse(
                wallet.getId(),
                wallet.getOwnerName(),
                wallet.getBalance()
        );
    }

    @Override
    public BalanceResponse getBalance(
            UUID walletId) {

        Wallet wallet =
                walletRepository.findById(walletId)
                        .orElseThrow(() ->
                                new WalletNotFoundException(
                                        "Wallet not found"));

        return new BalanceResponse(
                wallet.getId(),
                wallet.getBalance()
        );
    }

    @Override
    @Transactional(readOnly = true)
    public List<TransactionHistoryResponse>
    getTransactionHistory(
            UUID walletId) {

        Wallet wallet =
                walletRepository.findById(walletId)
                        .orElseThrow(() ->
                                new WalletNotFoundException(
                                        "Wallet not found"));

        List<LedgerEntry> entries =
                ledgerEntryRepository
                        .findTransactionHistory(walletId);

        return entries.stream()
                .map(entry -> {

                    UUID counterPartyWalletId;

                    if (entry.getEntryType() ==
                            EntryType.DEBIT) {

                        counterPartyWalletId =
                                entry.getTransfer()
                                        .getToWallet()
                                        .getId();

                    } else {

                        counterPartyWalletId =
                                entry.getTransfer()
                                        .getFromWallet()
                                        .getId();
                    }

                    return new TransactionHistoryResponse(
                            entry.getTransfer().getId(),
                            entry.getEntryType(),
                            entry.getAmount(),
                            counterPartyWalletId,
                            entry.getCreatedAt()
                    );
                })
                .toList();
    }
}