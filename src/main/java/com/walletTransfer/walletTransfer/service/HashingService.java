package com.walletTransfer.walletTransfer.service;

import com.walletTransfer.walletTransfer.dto.TransferRequest;

public interface HashingService {
    String generateHash(TransferRequest request);
}
