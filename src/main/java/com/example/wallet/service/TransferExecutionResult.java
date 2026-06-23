package com.example.wallet.service;

import com.example.wallet.web.dto.TransferResponse;

public record TransferExecutionResult(int statusCode, TransferResponse body) {}
