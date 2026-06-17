package com.siddharth.wallet.service;

import com.siddharth.wallet.dto.CreateTransferRequest;
import com.siddharth.wallet.dto.TransferResponse;

public interface TransferService {

    TransferResponse transfer(CreateTransferRequest request);
}
