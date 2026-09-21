package com.example.wallet.service;

import com.example.wallet.api.TransferRequest;
import com.example.wallet.api.TransferResponse;
import com.example.wallet.api.WalletBalanceResponse;
import com.example.wallet.domain.Transfer;
import com.example.wallet.domain.Wallet;
import com.example.wallet.exception.IdempotencyConflictException;
import com.example.wallet.exception.InvalidTransferException;
import com.example.wallet.exception.ValidationException;
import com.example.wallet.exception.WalletNotFoundException;
import com.example.wallet.repository.TransferRepository;
import com.example.wallet.repository.WalletRepository;
import jakarta.persistence.EntityManager;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

@Service
public class WalletService  implements TransferService {
    private final TransferRepository transferRepository;
    private final WalletRepository walletRepository;
    private final TransferTransactionService transactionService;
    private final EntityManager entityManager;
    private static final Logger logger = LoggerFactory.getLogger(WalletService.class);


    public WalletService (TransferRepository transferRepository,
                         WalletRepository walletRepository,
                         TransferTransactionService transactionService,
                         EntityManager entityManager) {
        this.transferRepository = transferRepository;
        this.walletRepository=walletRepository;
        this.transactionService = transactionService;
        this.entityManager = entityManager;
    }

    public WalletBalanceResponse getBalance(String walletId) {

        logger.debug("Looking up wallet balance walletId={}", walletId);
        Wallet wallet = walletRepository.findById(walletId)
                .orElseThrow(() ->
                        new WalletNotFoundException(walletId));

        logger.debug("Fetched wallet balance with walletId={} balance={}", walletId, wallet.getBalance());

        return new WalletBalanceResponse(
                wallet.getWalletId(),
                wallet.getBalance());
    }

    @Override
    public TransferResponse transfer(TransferRequest transferRequest) {
        validateRequest(transferRequest);
        // Fast path for normal retries. The unique DB constraint is still the
        // final authority when two requests with the same key race.
        var existing = transferRepository.findByIdempotencyKey(transferRequest.idempotencyKey());
        if (existing.isPresent()) {
            assertSameRequest(existing.get(), transferRequest);
            return TransferResponse.from(existing.get());
        }
        logger.debug("No existing transfer found for idempotency key {}, processing new transfer",
                transferRequest.idempotencyKey());

        try {
            return transactionService.processNew(transferRequest);
        } catch (DataIntegrityViolationException race) {
            // The losing concurrent request may have failed on the unique
            // idempotency constraint. Its transaction has already rolled back.
            entityManager.clear();
            var winner = transferRepository.findByIdempotencyKey(transferRequest.idempotencyKey())
                .orElseThrow(() -> race);
            assertSameRequest(winner, transferRequest);
            return TransferResponse.from(winner);
        }
    }

    @Override
    public void validateRequest(TransferRequest r) {
        if (r.fromWalletId().equals(r.toWalletId())) {
            logger.error("fromWalletId {} and toWalletId {} are same",r.fromWalletId(),r.toWalletId());
            throw new InvalidTransferException(
                    "fromWalletId and toWalletId must differ");
        }
        if (r.amount().scale() > 2) {
            logger.error("amount {} value supported with at most 2 decimal places", r.amount());
            throw new ValidationException(
                    "amount supports at most 2 decimal places");
        }
    }

    private void assertSameRequest(Transfer t, TransferRequest r) {

        if (!t.getFromWalletId().equals(r.fromWalletId())
            || !t.getToWalletId().equals(r.toWalletId())
            || t.getAmount().compareTo(r.amount()) != 0) {
            logger.error("Idempotency key {} was reused with different transfer parameters",r.idempotencyKey());
            throw new IdempotencyConflictException(
                    "Idempotency key was reused with different transfer parameters");
        }
        logger.info("Idempotency key {} was reused with same transfer parameters so previous response returned",r.idempotencyKey());
    }
}
