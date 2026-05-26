package com.wallet.service;

import com.wallet.domain.LedgerEntry;
import com.wallet.dto.WalletResponse;
import com.wallet.exception.WalletNotFoundException;
import com.wallet.repository.LedgerEntryRepository;
import com.wallet.repository.WalletRepository;
import java.util.List;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class WalletServiceImpl implements WalletService {

    private final WalletRepository walletRepository;
    private final LedgerEntryRepository ledgerEntryRepository;

    public WalletServiceImpl(WalletRepository walletRepository, LedgerEntryRepository ledgerEntryRepository) {
        this.walletRepository = walletRepository;
        this.ledgerEntryRepository = ledgerEntryRepository;
    }

    @Override
    @Transactional(readOnly = true)
    public WalletResponse getWallet(String walletId) {
        return walletRepository.findById(walletId)
            .map(WalletResponse::from)
            .orElseThrow(() -> new WalletNotFoundException(walletId));
    }

    @Override
    @Transactional(readOnly = true)
    public List<LedgerEntry> getLedgerEntries(String walletId) {
        if (!walletRepository.existsById(walletId)) {
            throw new WalletNotFoundException(walletId);
        }
        return ledgerEntryRepository.findByWalletIdOrderByCreatedAtDesc(walletId);
    }
}
