package com.wallettransfer.service;

import com.wallettransfer.domain.Wallet;
import com.wallettransfer.repository.WalletRepository;
import com.wallettransfer.service.exception.NotFoundException;
import com.wallettransfer.web.dto.CreateWalletRequest;
import com.wallettransfer.web.dto.WalletResponse;
import java.time.Instant;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

/**
 * Business operations for wallet lifecycle and balance reads.
 * Used for seeding test/demo data and optional balance API.
 */
@Service
public class WalletService {

    private final WalletRepository walletRepository;

    /**
     * @param walletRepository persistence for wallet entities
     */
    public WalletService(WalletRepository walletRepository) {
        this.walletRepository = walletRepository;
    }

    /**
     * Creates a wallet with the given id and initial balance.
     *
     * @param request wallet id and starting balance (must be &gt;= 0)
     * @return created wallet as API response
     */
    @Transactional
    public WalletResponse createWallet(CreateWalletRequest request) {
        Instant now = Instant.now();
        Wallet wallet = new Wallet(request.id(), request.initialBalance(), now, now);
        walletRepository.save(wallet);
        return WalletResponse.from(wallet);
    }

    /**
     * Returns the current balance and metadata for a wallet.
     *
     * @param walletId wallet identifier
     * @return wallet API response
     * @throws NotFoundException if the wallet does not exist
     */
    @Transactional(readOnly = true)
    public WalletResponse getWallet(String walletId) {
        Wallet wallet = walletRepository
                .findById(walletId)
                .orElseThrow(() -> new NotFoundException("Wallet not found: " + walletId));
        return WalletResponse.from(wallet);
    }
}
