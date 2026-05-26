package com.wallet.service;

import com.wallet.dto.TransferExecutionResult;
import com.wallet.dto.TransferRequest;
import com.wallet.dto.TransferResponse;
import java.util.UUID;

public interface TransferService {

    TransferExecutionResult executeTransfer(TransferRequest request);

    TransferResponse getTransfer(UUID transferId);
}
