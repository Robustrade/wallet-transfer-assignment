package com.example.wallet_transfer_assignment.service;

import com.example.wallet_transfer_assignment.dto.TransferRequest;
import com.example.wallet_transfer_assignment.dto.TransferResponse;

public interface TransferService {


    TransferResponse transfer(TransferRequest request);
}