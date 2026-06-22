package com.walletTransfer.walletTransfer.service;

import com.walletTransfer.walletTransfer.dto.TransferRequest;
import com.walletTransfer.walletTransfer.dto.TransferResponse;

public interface TransferService {
    TransferResponse createTransfer(TransferRequest request);
}
