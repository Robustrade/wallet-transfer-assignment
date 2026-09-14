package com.robustrade.wallet.service;

import com.robustrade.wallet.handler.dto.CreateTransferRequest;
import com.robustrade.wallet.handler.dto.TransferResponse;

public interface TransferService {

    TransferResponse createTransfer(CreateTransferRequest request);
}
