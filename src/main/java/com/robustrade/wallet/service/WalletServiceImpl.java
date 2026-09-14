package com.robustrade.wallet.service;

import com.robustrade.wallet.domain.LedgerEntry;
import com.robustrade.wallet.domain.Wallet;
import com.robustrade.wallet.handler.dto.CreateWalletRequest;
import com.robustrade.wallet.handler.dto.WalletResponse;
import com.robustrade.wallet.repository.LedgerEntryRepository;
import com.robustrade.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Slf4j
@Service
@RequiredArgsConstructor
public class WalletServiceImpl implements WalletService {

    private final WalletRepository walletRepository;
    private final LedgerEntryRepository ledgerEntryRepository;

    @Override
    @Transactional
    public WalletResponse createWallet(CreateWalletRequest request) {
        log.info(
                "Creating wallet for userId={} with initial balance={}",
                request.getUserId(),
                request.getBalance()
        );
        log.debug("Persisting new wallet entity for userId={}", request.getUserId());

        try {
            if (walletRepository.existsByUserId(request.getUserId())) {
                log.error("Wallet already exists for userId={}", request.getUserId());
                throw WalletServiceException.walletAlreadyExists(request.getUserId());
            }

            Wallet wallet = Wallet.create(request.getUserId(), request.getBalance());
            try {
                // Flush so unique(user_id) violations surface here, not on TX commit.
                walletRepository.saveAndFlush(wallet);
            } catch (DataIntegrityViolationException ex) {
                // Concurrent create: existsByUserId raced; unique(wallets_user_id_uk) rejected insert.
                if (isUserIdUniqueViolation(ex)) {
                    log.info(
                            "Unique user_id conflict while creating wallet for userId={}",
                            request.getUserId()
                    );
                    throw WalletServiceException.walletAlreadyExists(request.getUserId());
                }
                throw ex;
            }

            recordInitialFunding(wallet);

            log.info(
                    "Wallet persisted successfully: walletId={}, userId={}, balance={}",
                    wallet.getId(),
                    wallet.getUserId(),
                    wallet.getBalance()
            );
            log.debug(
                    "Wallet entity details: id={}, userId={}, balance={}, createdAt={}",
                    wallet.getId(),
                    wallet.getUserId(),
                    wallet.getBalance(),
                    wallet.getCreatedAt()
            );

            return WalletResponse.from(wallet);
        } catch (WalletServiceException ex) {
            log.error(
                    "Error while creating wallet for userId={}: {}",
                    request.getUserId(),
                    ex.getMessage()
            );
            throw ex;
        } catch (Exception ex) {
            log.error(
                    "Error while creating wallet for userId={}: {}",
                    request.getUserId(),
                    ex.getMessage(),
                    ex
            );
            throw ex;
        }
    }

    private void recordInitialFunding(Wallet wallet) {
        if (wallet.getBalance().compareTo(BigDecimal.ZERO) <= 0) {
            return;
        }
        LedgerEntry funding = LedgerEntry.initialFunding(wallet.getId(), wallet.getBalance());
        ledgerEntryRepository.save(funding);
        log.debug(
                "Recorded opening-balance funding ledger entry: walletId={}, amount={}, ledgerId={}",
                wallet.getId(),
                wallet.getBalance(),
                funding.getId()
        );
    }

    private static boolean isUserIdUniqueViolation(DataIntegrityViolationException ex) {
        Throwable cause = ex.getMostSpecificCause();
        String message = cause != null ? cause.getMessage() : ex.getMessage();
        return message != null && message.contains("wallets_user_id_uk");
    }
}
