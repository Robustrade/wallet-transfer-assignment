package com.example.wallettransfer.service;

import com.example.wallettransfer.dto.CreateTransferRequest;
import com.example.wallettransfer.dto.TransferResponse;

public interface TransferService {

    TransferResponse createTransfer(CreateTransferRequest request);
}
