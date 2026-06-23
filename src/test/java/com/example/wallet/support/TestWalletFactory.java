package com.example.wallet.support;

import com.example.wallet.domain.Wallet;
import com.example.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import org.springframework.stereotype.Component;
import org.springframework.transaction.annotation.Transactional;

@Component
public class TestWalletFactory {

    private final WalletRepository walletRepository;

    public TestWalletFactory(WalletRepository walletRepository) {
        this.walletRepository = walletRepository;
    }

    @Transactional
    public Wallet createWallet(String id, BigDecimal balance) {
        return walletRepository.save(new Wallet(id, balance));
    }
}
