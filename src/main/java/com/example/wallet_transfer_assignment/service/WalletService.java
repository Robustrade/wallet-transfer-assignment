package com.example.wallet_transfer_assignment.service;

import com.example.wallet_transfer_assignment.dto.BalanceResponse;
import com.example.wallet_transfer_assignment.dto.CreateWalletRequest;
import com.example.wallet_transfer_assignment.dto.TransactionHistoryResponse;
import com.example.wallet_transfer_assignment.dto.WalletResponse;

import java.util.List;
import java.util.UUID;

public interface WalletService {

    WalletResponse createWallet(
            CreateWalletRequest request);

    BalanceResponse getBalance(
            UUID walletId);

    List<TransactionHistoryResponse>
    getTransactionHistory(
            UUID walletId);
}