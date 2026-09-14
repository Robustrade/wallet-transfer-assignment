package com.robustrade.wallet.service;

import com.robustrade.wallet.handler.dto.StatementResponse;

public interface StatementService {

    StatementResponse getStatement(Long userId, String walletId);
}
