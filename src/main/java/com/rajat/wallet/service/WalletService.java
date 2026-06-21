package com.rajat.wallet.service;

import com.rajat.wallet.dto.TransferResponse;
import com.rajat.wallet.dto.WalletResponse;
import java.util.UUID;
import org.springframework.data.domain.Page;
import org.springframework.data.domain.Pageable;

/** Read-side queries for wallets: current balance and transfer history. */
public interface WalletService {

  /** Current balance of a wallet. Throws {@code WalletNotFoundException} if it does not exist. */
  WalletResponse getWallet(UUID walletId);

  /**
   * A page of transfers in which the wallet participated (as source or destination). Throws {@code
   * WalletNotFoundException} if the wallet does not exist.
   */
  Page<TransferResponse> getTransferHistory(UUID walletId, Pageable pageable);
}
