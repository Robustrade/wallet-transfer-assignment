package com.walletTransfer.walletTransfer.service.impl;

import com.walletTransfer.walletTransfer.dto.TransferResponse;

import com.walletTransfer.walletTransfer.entity.IdempotencyRecord;
import com.walletTransfer.walletTransfer.entity.Transfer;
import com.walletTransfer.walletTransfer.entity.Wallet;
import com.walletTransfer.walletTransfer.exception.DeadlockRetryException;
import com.walletTransfer.walletTransfer.repository.LedgerEntryRepository;
import com.walletTransfer.walletTransfer.service.TransferDomainService;
import com.walletTransfer.walletTransfer.dto.TransferRequest;
import com.walletTransfer.walletTransfer.entity.LedgerEntry;
import com.walletTransfer.walletTransfer.dto.TransferResponse;
import com.walletTransfer.walletTransfer.enums.TransferStatus;
import com.walletTransfer.walletTransfer.exception.WalletNotFoundException;
import com.walletTransfer.walletTransfer.mapper.TransferMapper;
import com.walletTransfer.walletTransfer.repository.TransferRepository;
import com.walletTransfer.walletTransfer.repository.WalletRepository;
import com.walletTransfer.walletTransfer.service.HashingService;
import com.walletTransfer.walletTransfer.service.IdempotencyService;
import com.walletTransfer.walletTransfer.service.TransferService;
import com.walletTransfer.walletTransfer.util.WalletLockHelper;
import lombok.RequiredArgsConstructor;
import org.springframework.dao.CannotAcquireLockException;
import org.springframework.retry.annotation.Backoff;
import org.springframework.retry.annotation.Recover;
import org.springframework.retry.annotation.Retryable;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.util.List;
import java.util.UUID;

@Service
@RequiredArgsConstructor
public class TransferServiceImpl implements TransferService {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;

    private final TransferDomainService transferDomainService;
    private final IdempotencyService idempotencyService;
    private final HashingService hashingService;
    private final TransferMapper transferMapper;

    @Override
    @Transactional
    @Retryable(
            retryFor = CannotAcquireLockException.class,
            maxAttempts = 3,
            backoff = @Backoff(delay = 200)
    )
    public TransferResponse createTransfer(
            TransferRequest request) {

        String requestHash =
                hashingService.generateHash(request);

        var existingResponse =
                idempotencyService.findExistingResponse(
                        request.getIdempotencyKey(),
                        requestHash);

        if (existingResponse.isPresent()) {
            return existingResponse.get();
        }

        IdempotencyRecord idempotencyRecord =
                idempotencyService.createPendingRecord(
                        request.getIdempotencyKey(),
                        requestHash);

        List<UUID> walletIds =
                WalletLockHelper.sortWalletIds(
                        request.getFromWalletId(),
                        request.getToWalletId());

        List<Wallet> wallets =
                walletRepository.findAndLockWallets(
                        walletIds);

        Wallet sourceWallet =
                wallets.stream()
                        .filter(wallet ->
                                wallet.getId().equals(
                                        request.getFromWalletId()))
                        .findFirst()
                        .orElseThrow(() ->
                                new WalletNotFoundException(
                                        "Source wallet not found"));

        Wallet destinationWallet =
                wallets.stream()
                        .filter(wallet ->
                                wallet.getId().equals(
                                        request.getToWalletId()))
                        .findFirst()
                        .orElseThrow(() ->
                                new WalletNotFoundException(
                                        "Destination wallet not found"));

        Transfer transfer =
                Transfer.builder()
                        .id(UUID.randomUUID())
                        .fromWallet(sourceWallet)
                        .toWallet(destinationWallet)
                        .amount(request.getAmount())
                        .status(TransferStatus.PENDING)
                        .build();

        transferRepository.save(transfer);

        try {

            List<LedgerEntry> ledgerEntries =
                    transferDomainService.processTransfer(
                            sourceWallet,
                            destinationWallet,
                            transfer);

            walletRepository.save(sourceWallet);
            walletRepository.save(destinationWallet);

            ledgerEntryRepository.saveAll(
                    ledgerEntries);

            transferRepository.save(transfer);

            TransferResponse response =
                    transferMapper.toResponse(
                            transfer);

            idempotencyService.markCompleted(idempotencyRecord,
                    transfer,
                    response);

            return response;

        } catch (Exception ex) {

            transfer.markFailed(
                    ex.getMessage());

            transferRepository.save(
                    transfer);

            idempotencyService.markFailed(idempotencyRecord);

            throw ex;
        }
    }

    @Recover
    public TransferResponse recover(
            CannotAcquireLockException ex,
            TransferRequest request) {

        throw new DeadlockRetryException(
                "Transfer failed after maximum retry attempts",
                ex);
    }
}
