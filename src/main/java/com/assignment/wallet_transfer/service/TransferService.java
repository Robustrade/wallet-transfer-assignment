package com.assignment.wallet_transfer.service;

import com.assignment.wallet_transfer.dtos.TransferHistoryResponse;
import com.assignment.wallet_transfer.dtos.TransferRequest;
import com.assignment.wallet_transfer.dtos.TransferResponse;

import java.util.List;
import java.util.UUID;

public interface TransferService {


    TransferResponse transfer(TransferRequest transferRequest, String idempotencyKey);

    List<TransferHistoryResponse> getWalletTransfers(UUID sourceWalletId);
}
