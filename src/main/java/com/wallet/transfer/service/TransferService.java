package com.wallet.transfer.service;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.transfer.dto.TransferRequest;
import com.wallet.transfer.dto.TransferResponse;
import com.wallet.transfer.entity.*;
import com.wallet.transfer.exception.ConflictException;
import com.wallet.transfer.repository.LedgerEntryRepository;
import com.wallet.transfer.repository.TransferRepository;
import com.wallet.transfer.repository.WalletRepository;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.math.BigDecimal;
import java.util.Arrays;
import java.util.List;

@Service
@RequiredArgsConstructor
public class TransferService {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyService idempotencyService;
    private final ObjectMapper objectMapper;

    public TransferResponse executeTransfer(TransferRequest request) {
        // 1. Check Idempotency
        IdempotencyRecord idempotencyRecord = idempotencyService.checkAndInitializeIdempotencyRecord(request);

        // If it was already processed, return the stored response
        if (idempotencyRecord.getResponseStatus() != null) {
            try {
                return objectMapper.readValue(idempotencyRecord.getResponseBody(), TransferResponse.class);
            } catch (JsonProcessingException e) {
                throw new RuntimeException("Failed to deserialize response", e);
            }
        }

        // 2. Process Transfer in a new transaction
        TransferResponse response;
        int status;
        try {
            response = processTransferInternal(request);
            status = response.getState() == TransferState.FAILED ? 400 : 201; // Example status codes
        } catch (Exception e) {
            response = TransferResponse.builder()
                    .fromWalletId(request.getFromWalletId())
                    .toWalletId(request.getToWalletId())
                    .amount(request.getAmount())
                    .state(TransferState.FAILED)
                    .message("Internal error: " + e.getMessage())
                    .build();
            status = 500;
        }

        // 3. Update Idempotency Record
        idempotencyService.updateIdempotencyRecord(request.getIdempotencyKey(), response, status);

        return response;
    }

    @Transactional
    protected TransferResponse processTransferInternal(TransferRequest request) {
        String fromId = request.getFromWalletId();
        String toId = request.getToWalletId();
        BigDecimal amount = request.getAmount();

        if (fromId.equals(toId)) {
            return buildResponse(request, null, TransferState.FAILED, "Cannot transfer to the same wallet");
        }

        // Lock wallets in alphabetical order to avoid deadlocks
        List<String> sortedIds = fromId.compareTo(toId) < 0 ? Arrays.asList(fromId, toId) : Arrays.asList(toId, fromId);
        List<Wallet> lockedWallets = walletRepository.findByIdsForUpdateOrdered(sortedIds);

        if (lockedWallets.size() != 2) {
            return buildResponse(request, null, TransferState.FAILED, "One or both wallets not found");
        }

        Wallet fromWallet = lockedWallets.stream().filter(w -> w.getId().equals(fromId)).findFirst().get();
        Wallet toWallet = lockedWallets.stream().filter(w -> w.getId().equals(toId)).findFirst().get();

        if (fromWallet.getBalance().compareTo(amount) < 0) {
            Transfer failedTransfer = createTransferRecord(request, TransferState.FAILED);
            return buildResponse(request, failedTransfer.getId(), TransferState.FAILED, "Insufficient funds");
        }

        // Execute Transfer
        fromWallet.setBalance(fromWallet.getBalance().subtract(amount));
        toWallet.setBalance(toWallet.getBalance().add(amount));

        walletRepository.saveAll(Arrays.asList(fromWallet, toWallet));

        Transfer processedTransfer = createTransferRecord(request, TransferState.PROCESSED);

        // Create Ledger Entries
        LedgerEntry debit = LedgerEntry.builder()
                .walletId(fromId)
                .transferId(processedTransfer.getId())
                .entryType("DEBIT")
                .amount(amount)
                .build();

        LedgerEntry credit = LedgerEntry.builder()
                .walletId(toId)
                .transferId(processedTransfer.getId())
                .entryType("CREDIT")
                .amount(amount)
                .build();

        ledgerEntryRepository.saveAll(Arrays.asList(debit, credit));

        return buildResponse(request, processedTransfer.getId(), TransferState.PROCESSED, "Transfer successful");
    }

    private Transfer createTransferRecord(TransferRequest request, TransferState state) {
        Transfer transfer = Transfer.builder()
                .fromWalletId(request.getFromWalletId())
                .toWalletId(request.getToWalletId())
                .amount(request.getAmount())
                .state(state)
                .build();
        return transferRepository.save(transfer);
    }

    private TransferResponse buildResponse(TransferRequest request, String transferId, TransferState state, String message) {
        return TransferResponse.builder()
                .transferId(transferId)
                .fromWalletId(request.getFromWalletId())
                .toWalletId(request.getToWalletId())
                .amount(request.getAmount())
                .state(state)
                .message(message)
                .build();
    }
}
