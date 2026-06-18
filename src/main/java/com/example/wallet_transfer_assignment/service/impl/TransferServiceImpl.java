package com.example.wallet_transfer_assignment.service.impl;

import com.example.wallet_transfer_assignment.dto.TransferRequest;
import com.example.wallet_transfer_assignment.dto.TransferResponse;
import com.example.wallet_transfer_assignment.entity.*;
import com.example.wallet_transfer_assignment.exception.InsufficientBalanceException;
import com.example.wallet_transfer_assignment.exception.InvalidTransferException;
import com.example.wallet_transfer_assignment.exception.WalletNotFoundException;
import com.example.wallet_transfer_assignment.repository.IdempotencyRecordRepository;
import com.example.wallet_transfer_assignment.repository.LedgerEntryRepository;
import com.example.wallet_transfer_assignment.repository.TransferRepository;
import com.example.wallet_transfer_assignment.repository.WalletRepository;
import com.example.wallet_transfer_assignment.service.TransferService;


import com.fasterxml.jackson.core.JsonProcessingException;

import com.fasterxml.jackson.databind.ObjectMapper;
import lombok.RequiredArgsConstructor;
import org.springframework.data.redis.core.RedisTemplate;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.transaction.support.TransactionSynchronization;
import org.springframework.transaction.support.TransactionSynchronizationManager;


import java.util.Optional;
import java.util.UUID;
import java.util.concurrent.TimeUnit;

@Service
@RequiredArgsConstructor
public class TransferServiceImpl implements TransferService {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final RedisTemplate<String, String> redisTemplate;

    private static final ObjectMapper OBJECT_MAPPER =
            new ObjectMapper();
    private static final String IDEMPOTENCY_PREFIX = "idempotency:";
    private static final long CACHE_TTL_HOURS = 24;

    @Override
    @Transactional
    public TransferResponse transfer(TransferRequest request) {

        // 1. Validate wallets
        if (request.fromWalletId().equals(request.toWalletId())) {
            throw new InvalidTransferException(
                    "Source and destination wallet cannot be same"
            );
        }

        String redisKey = IDEMPOTENCY_PREFIX + request.idempotencyKey();
        String cachedTransferId = redisTemplate.opsForValue().get(redisKey);
        if (cachedTransferId != null) {

            Transfer existingTransfer =
                    transferRepository.findById(
                                    UUID.fromString(cachedTransferId))
                            .orElseThrow(() ->
                                    new RuntimeException(
                                            "Transfer not found"));

            return buildResponse(existingTransfer);
        }

        Optional<IdempotencyRecord> existingRecord =
                idempotencyRecordRepository
                        .findByIdempotencyKey(
                                request.idempotencyKey());

        if (existingRecord.isPresent()) {

            Transfer existingTransfer =
                    existingRecord.get().getTransfer();

            redisTemplate.opsForValue().set(
                    redisKey,
                    existingTransfer.getId().toString(),
                    CACHE_TTL_HOURS,
                    TimeUnit.HOURS
            );

            return buildResponse(existingTransfer);
        }

        // 3. Lock wallets in deterministic order
        UUID firstWalletId =
                request.fromWalletId().compareTo(request.toWalletId()) < 0
                        ? request.fromWalletId()
                        : request.toWalletId();

        UUID secondWalletId =
                request.fromWalletId().compareTo(request.toWalletId()) < 0
                        ? request.toWalletId()
                        : request.fromWalletId();

        Wallet firstLockedWallet =
                walletRepository.findByIdForUpdate(firstWalletId)
                        .orElseThrow(() ->
                                new WalletNotFoundException(
                                        "Wallet not found : " + firstWalletId));

        Wallet secondLockedWallet =
                walletRepository.findByIdForUpdate(secondWalletId)
                        .orElseThrow(() ->
                                new WalletNotFoundException(
                                        "Wallet not found : " + secondWalletId));

        Wallet fromWallet =
                firstLockedWallet.getId().equals(request.fromWalletId())
                        ? firstLockedWallet
                        : secondLockedWallet;

        Wallet toWallet =
                secondLockedWallet.getId().equals(request.toWalletId())
                        ? secondLockedWallet
                        : firstLockedWallet;

        // 4. Balance validation
        if (fromWallet.getBalance().compareTo(request.amount()) < 0) {
            throw new InsufficientBalanceException(
                    "Insufficient balance"
            );
        }

        // 5. Create Transfer
        Transfer transfer = Transfer.builder()
                .fromWallet(fromWallet)
                .toWallet(toWallet)
                .amount(request.amount())
                .state(TransferState.PENDING)
                .build();

        transferRepository.save(transfer);

        // 6. Update balances
        fromWallet.setBalance(
                fromWallet.getBalance().subtract(request.amount())
        );

        toWallet.setBalance(
                toWallet.getBalance().add(request.amount())
        );

        walletRepository.save(fromWallet);
        walletRepository.save(toWallet);

        // 7. Create ledger entries
        LedgerEntry debitEntry = LedgerEntry.builder()
                .transfer(transfer)
                .wallet(fromWallet)
                .entryType(EntryType.DEBIT)
                .amount(request.amount())
                .build();

        LedgerEntry creditEntry = LedgerEntry.builder()
                .transfer(transfer)
                .wallet(toWallet)
                .entryType(EntryType.CREDIT)
                .amount(request.amount())
                .build();

        ledgerEntryRepository.save(debitEntry);
        ledgerEntryRepository.save(creditEntry);

        // 8. Mark transfer processed
        transfer.setState(TransferState.PROCESSED);

        transferRepository.save(transfer);

        // 9. Prepare response
        TransferResponse response = buildResponse(transfer);

        // 10. Save idempotency record
        try {

            IdempotencyRecord record =
                    IdempotencyRecord.builder()
                            .idempotencyKey(request.idempotencyKey())
                            .transfer(transfer)
                            .responseJson(
                                    OBJECT_MAPPER.writeValueAsString(response)
                            )
                            .build();

            idempotencyRecordRepository.save(record);

            // Register a post-commit callback to write Redis only after the DB transaction successfully commits.
            if (TransactionSynchronizationManager.isSynchronizationActive()) {
                TransactionSynchronizationManager.registerSynchronization(new TransactionSynchronization() {
                    @Override
                    public void afterCommit() {
                        redisTemplate.opsForValue().set(
                                redisKey,
                                transfer.getId().toString(),
                                CACHE_TTL_HOURS,
                                TimeUnit.HOURS
                        );
                    }
                });
            } else {
                // If no transaction is active, write immediately.
                redisTemplate.opsForValue().set(
                        redisKey,
                        transfer.getId().toString(),
                        CACHE_TTL_HOURS,
                        TimeUnit.HOURS
                );
            }

        } catch (JsonProcessingException e) {
            throw new RuntimeException(
                    "Failed to serialize response", e);
        }

        return response;
    }

    private TransferResponse buildResponse(
            Transfer transfer) {

        return new TransferResponse(
                transfer.getId(),
                transfer.getFromWallet().getId(),
                transfer.getToWallet().getId(),
                transfer.getAmount(),
                transfer.getState()
        );
    }
}