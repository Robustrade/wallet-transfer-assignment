package com.example.wallet.service;

import com.example.wallet.api.TransferRequest;
import com.example.wallet.api.TransferResponse;

public interface TransferService {
    void validateRequest(TransferRequest request);
    TransferResponse transfer(TransferRequest request);
}
