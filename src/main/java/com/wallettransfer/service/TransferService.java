package com.wallettransfer.service;

import com.wallettransfer.api.TransferRequest;
import com.wallettransfer.api.TransferResponse;
import com.wallettransfer.api.WalletResponse;
import com.wallettransfer.domain.LedgerEntryType;
import com.wallettransfer.repository.LedgerRepository;
import com.wallettransfer.repository.TransferRepository;
import com.wallettransfer.repository.WalletRepository;
import org.springframework.dao.EmptyResultDataAccessException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.math.BigDecimal;
import java.util.UUID;

@Service
public class TransferService {
    private final WalletRepository wallets;
    private final TransferRepository transfers;
    private final LedgerRepository ledger;

    public TransferService(WalletRepository wallets, TransferRepository transfers, LedgerRepository ledger) {
        this.wallets = wallets;
        this.transfers = transfers;
        this.ledger = ledger;
    }

    @Transactional
    public TransferResponse create(TransferRequest request) {
        validate(request);
        UUID transferId = UUID.randomUUID();
        boolean claimed = transfers.claimIdempotency(request.idempotencyKey(), transferId, request.fromWalletId(),
                request.toWalletId(), request.amount());
        TransferResponse existing = transfers.findByIdempotencyKeyWithLock(request.idempotencyKey());
        if (!claimed) {
            if (!sameRequest(existing, request)) {
                throw new InvalidTransferException("Idempotency key was already used with different transfer data");
            }
            if (existing.status() != com.wallettransfer.domain.TransferStatus.PENDING) {
                return existing;
            }
            transferId = existing.transferId();
        }

        try {
            WalletResponse source;
            WalletResponse destination;
            if (request.fromWalletId().compareTo(request.toWalletId()) < 0) {
                source = lockWallet(request.fromWalletId());
                destination = lockWallet(request.toWalletId());
            } else {
                destination = lockWallet(request.toWalletId());
                source = lockWallet(request.fromWalletId());
            }
            if (source.balance().compareTo(request.amount()) < 0) {
                throw new InsufficientFundsException("Insufficient funds in wallet: " + source.walletId());
            }
            wallets.updateBalance(source.walletId(), source.balance().subtract(request.amount()));
            wallets.updateBalance(destination.walletId(), destination.balance().add(request.amount()));
            ledger.add(transferId, source.walletId(), LedgerEntryType.DEBIT, request.amount());
            ledger.add(transferId, destination.walletId(), LedgerEntryType.CREDIT, request.amount());
            transfers.markProcessed(transferId);
            return transfers.findById(transferId);
        } catch (RuntimeException exception) {
            transfers.markFailed(transferId, exception.getMessage());
            throw exception;
        }
    }

    @Transactional(readOnly = true)
    public WalletResponse getWallet(String walletId) {
        try {
            return wallets.findById(walletId);
        } catch (EmptyResultDataAccessException exception) {
            throw new WalletNotFoundException(walletId);
        }
    }

    @Transactional
    public WalletResponse createWallet(BigDecimal initialBalance) {
        if (initialBalance.scale() > 4 || initialBalance.compareTo(BigDecimal.ZERO) < 0) {
            throw new InvalidTransferException("Initial balance must be non-negative with at most four decimal places");
        }
        return wallets.create(initialBalance);
    }

    private WalletResponse lockWallet(String walletId) {
        try {
            return wallets.findByIdForUpdate(walletId);
        } catch (EmptyResultDataAccessException exception) {
            throw new WalletNotFoundException(walletId);
        }
    }

    private void validate(TransferRequest request) {
        if (request.fromWalletId().equals(request.toWalletId())) {
            throw new InvalidTransferException("Source and destination wallets must be different");
        }
        if (request.amount().scale() > 4 || request.amount().compareTo(BigDecimal.ZERO) <= 0) {
            throw new InvalidTransferException("Amount must be positive with at most four decimal places");
        }
    }

    private boolean sameRequest(TransferResponse existing, TransferRequest request) {
        return existing != null && existing.fromWalletId().equals(request.fromWalletId())
                && existing.toWalletId().equals(request.toWalletId())
                && existing.amount().compareTo(request.amount()) == 0;
    }
}