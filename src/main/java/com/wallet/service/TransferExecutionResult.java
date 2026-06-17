package com.wallet.service;

import com.wallet.api.dto.TransferResponse;

public record TransferExecutionResult(TransferResponse response, int httpStatus) {}
