package com.example.wallettransfer.service;

import com.example.wallettransfer.dto.TransferHistoryResponse;
import com.example.wallettransfer.dto.WalletResponse;

public interface WalletQueryService {

    WalletResponse getWallet(String walletId);

    TransferHistoryResponse getTransferHistory(String walletId, int limit, String cursor);
}
