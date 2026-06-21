package com.rajat.wallet.service;

import com.rajat.wallet.domain.entities.Transfer;
import com.rajat.wallet.domain.entities.Wallet;
import com.rajat.wallet.dto.TransferResponse;
import com.rajat.wallet.dto.WalletResponse;
import com.rajat.wallet.exception.WalletNotFoundException;
import com.rajat.wallet.repository.TransferRepository;
import com.rajat.wallet.repository.WalletRepository;
import java.util.UUID;
import lombok.RequiredArgsConstructor;
import org.springframework.data.domain.Page;
import org.springframework.data.domain.Pageable;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

/**
 * Read-only query side. Both methods run in a {@code readOnly} transaction so the entities are
 * mapped to DTOs while the session is open ({@code open-in-view} is disabled).
 */
@Service
@RequiredArgsConstructor
public class WalletServiceImpl implements WalletService {

  private final WalletRepository walletRepository;
  private final TransferRepository transferRepository;

  @Override
  @Transactional(readOnly = true)
  public WalletResponse getWallet(UUID walletId) {
    Wallet wallet =
        walletRepository
            .findById(walletId)
            .orElseThrow(() -> new WalletNotFoundException(walletId));
    return WalletResponse.from(wallet);
  }

  @Override
  @Transactional(readOnly = true)
  public Page<TransferResponse> getTransferHistory(UUID walletId, Pageable pageable) {
    Page<Transfer> page = transferRepository.findByWalletId(walletId, pageable);
    // Fetch first; only pay for the existence check when the page is empty, to tell a wallet with
    // no transfers (200, empty) apart from a wallet that doesn't exist (404).
    if (page.isEmpty() && !walletRepository.existsById(walletId)) {
      throw new WalletNotFoundException(walletId);
    }
    return page.map(TransferResponse::from);
  }
}
