package com.robustrade.wallet.service;

import com.robustrade.wallet.handler.dto.CreateWalletRequest;
import com.robustrade.wallet.handler.dto.WalletResponse;

public interface WalletService {

    WalletResponse createWallet(CreateWalletRequest request);
}
