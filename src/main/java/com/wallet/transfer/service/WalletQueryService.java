package com.wallet.transfer.service;

import com.wallet.transfer.domain.Transfer;
import com.wallet.transfer.domain.Wallet;
import com.wallet.transfer.repository.TransferRepository;
import com.wallet.transfer.repository.WalletRepository;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.util.Optional;

@Service
@Transactional(readOnly = true)
public class WalletQueryService {

    private final TransferRepository transferRepository;
    private final WalletRepository walletRepository;

    public WalletQueryService(TransferRepository transferRepository,
                              WalletRepository walletRepository) {
        this.transferRepository = transferRepository;
        this.walletRepository = walletRepository;
    }

    public Optional<Transfer> getTransfer(String transferId) {
        return transferRepository.findById(transferId);
    }

    public Optional<Wallet> getWallet(String walletId) {
        return walletRepository.findById(walletId);
    }
}
