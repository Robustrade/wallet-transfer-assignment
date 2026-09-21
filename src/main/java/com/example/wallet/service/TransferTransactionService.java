package com.example.wallet.service;

import com.example.wallet.api.TransferRequest;
import com.example.wallet.api.TransferResponse;
import com.example.wallet.domain.*;
import com.example.wallet.exception.WalletNotActiveException;
import com.example.wallet.exception.WalletNotFoundException;
import com.example.wallet.repository.LedgerEntryRepository;
import com.example.wallet.repository.TransferRepository;
import com.example.wallet.repository.WalletRepository;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

@Service
public class TransferTransactionService {
    private final TransferRepository transfers;
    private final WalletRepository wallets;
    private final LedgerEntryRepository ledger;
    private static final Logger logger = LoggerFactory.getLogger(TransferTransactionService.class);

    public TransferTransactionService(TransferRepository transfers,
                                      WalletRepository wallets,
                                      LedgerEntryRepository ledger) {
        this.transfers = transfers;
        this.wallets = wallets;
        this.ledger = ledger;
    }

    @Transactional
    public TransferResponse processNew(TransferRequest request) {

        // Create a new transfer record in the database for idempotency and auditing purposes.
        logger.debug("Processing transactional transfer request: {}", request);
        var transfer = new Transfer(request.idempotencyKey(), request.fromWalletId(),
                request.toWalletId(), request.amount());
        transfers.saveAndFlush(transfer);
        logger.debug("idempotency record created with id: {}, idempotency key: {}",transfer.getId(),transfer.getIdempotencyKey());

        // Load wallets in a consistent order to avoid deadlocks
        Wallet[] walletsInLockOrder = loadWalletsInLockOrder(request.fromWalletId(), request.toWalletId());
        Wallet source = findWallet(walletsInLockOrder, request.fromWalletId());
        Wallet destination = findWallet(walletsInLockOrder, request.toWalletId());

        //validation of wallet status before processing the transfer
        validateWalletStatus(source, destination);
        if (!source.hasSufficientBalance(request.amount())) {
            transfer.markFailed();
            transfers.save(transfer);

            logger.error("source wallet {} has insufficient balance: {}, " +
                            "But requested transfer amount is: {} ",
                    source.getWalletId(),source.getBalance(),request.amount());
            return TransferResponse.from(transfer);
        }

        logger.debug("all validations completed successfully");

        /* Perform the transfer by debiting the source wallet and
        crediting the destination wallet and saving the ledger and transfer entries for auditing purposes.*/
        source.debit(request.amount());
        destination.credit(request.amount());
        ledger.save(LedgerEntry.debit(source.getWalletId(), transfer.getId(), request.amount()));
        ledger.save(LedgerEntry.credit(destination.getWalletId(), transfer.getId(), request.amount()));
        transfer.markProcessed();
        transfers.save(transfer);

        logger.info("wallet , ledger, transfer entries are updated successfully in repo entities");
        return TransferResponse.from(transfer);
    }

    private void validateWalletStatus(
            Wallet source,
            Wallet destination
    ) {
        if (!source.isActive()) {
            logger.error("Source wallet {} is not active ",source.getWalletId());
            throw new WalletNotActiveException("Source wallet is not active: " + source.getWalletId());
        }

        if (!destination.isActive()) {
            logger.error("Destination wallet {} is not active ",destination.getWalletId());
            throw new WalletNotActiveException("Destination wallet is not active: " + destination.getWalletId());
        }
    }

    private Wallet[] loadWalletsInLockOrder(
            String fromWalletId,
            String toWalletId) {

        String firstId;
        String secondId;

        if (fromWalletId.compareTo(toWalletId) < 0) {
            firstId = fromWalletId;
            secondId = toWalletId;
        } else {
            firstId = toWalletId;
            secondId = fromWalletId;
        }

        // Load wallets in a consistent order to avoid deadlocks
        Wallet first = wallets.findByWalletId(firstId)
                .orElseThrow(() ->
                        new WalletNotFoundException(firstId));

        Wallet second = wallets.findByWalletId(secondId)
                .orElseThrow(() ->
                        new WalletNotFoundException(secondId));

        logger.info("wallet fetched wallet 1: {} and wallet 2: {}",
                first.getWalletId(),second.getWalletId());
        return new Wallet[]{first, second};
    }

    private Wallet findWallet(
            Wallet[] walletsInLockOrder,
            String walletId) {

        if (walletsInLockOrder[0].getWalletId().equals(walletId)) {
            return walletsInLockOrder[0];
        }

        return walletsInLockOrder[1];
    }
}
