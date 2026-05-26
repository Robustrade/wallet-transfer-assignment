package com.wallet.service;

import com.wallet.domain.LedgerEntry;
import com.wallet.dto.WalletResponse;
import java.util.List;

public interface WalletService {

    WalletResponse getWallet(String walletId);

    List<LedgerEntry> getLedgerEntries(String walletId);
}
