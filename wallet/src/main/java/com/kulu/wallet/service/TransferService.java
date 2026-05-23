package com.kulu.wallet.service;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.kulu.wallet.domain.Wallet;
import com.kulu.wallet.dto.TransferRequest;
import com.kulu.wallet.dto.TransferResponse;
import com.kulu.wallet.exception.WalletNotFoundException;
import com.kulu.wallet.repository.IdempotencyRepository;
import com.kulu.wallet.repository.LedgerRepository;
import com.kulu.wallet.repository.TransferRepository;
import com.kulu.wallet.repository.WalletRepository;
import java.util.Map;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Service;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.support.TransactionTemplate;

import java.util.List;
import java.util.UUID;

@Service
public class TransferService {

    @Autowired private WalletRepository walletRepo;
    @Autowired private TransferRepository transferRepo;
    @Autowired private LedgerRepository ledgerRepo;
    @Autowired private IdempotencyRepository idempotencyRepo;
    @Autowired private ObjectMapper objectMapper;

    private TransactionTemplate txTemplate;

    @Autowired
    public void setTxManager(PlatformTransactionManager txManager) {
        this.txTemplate = new TransactionTemplate(txManager);
    }

    public ResponseEntity<Object> transfer(TransferRequest request) {
        if (request.fromWalletId().equals(request.toWalletId())) {
            throw new IllegalArgumentException("fromWalletId and toWalletId must be different");
        }

        // Claim idempotency key — auto-committed, intentionally outside the transfer transaction
        boolean claimed = idempotencyRepo.claim(request.idempotencyKey());

        if (!claimed) {
            IdempotencyRepository.IdempotencyRecord existing =
                idempotencyRepo.find(request.idempotencyKey());
            if (existing != null && existing.responseStatus() != 0) {
                return fromRecord(existing);
            }
            return pollForResult(request.idempotencyKey());
        }

        return executeTransfer(request);
    }

    private ResponseEntity<Object> executeTransfer(TransferRequest request) {
        // Holds the serialized JSON body after the transaction commits.
        // Array used because lambda capture requires effectively-final references.
        String[] result = new String[1];

        try {
            txTemplate.execute(status -> {
                // Step 1: Lock both wallets in consistent ascending-id order (prevents deadlocks)
                List<Wallet> wallets = walletRepo.lockForUpdate(
                    request.fromWalletId(), request.toWalletId());

                Wallet fromWallet = wallets.stream()
                    .filter(w -> w.id() == request.fromWalletId())
                    .findFirst()
                    .orElseThrow(() -> new WalletNotFoundException(request.fromWalletId()));

                wallets.stream()
                    .filter(w -> w.id() == request.toWalletId())
                    .findFirst()
                    .orElseThrow(() -> new WalletNotFoundException(request.toWalletId()));

                // Step 2: Insert transfer as PENDING before any outcome is known
                String transferId = UUID.randomUUID().toString();
                transferRepo.insert(transferId, request.idempotencyKey(),
                    request.fromWalletId(), request.toWalletId(), request.amount());

                // Step 3: Balance check
                if (fromWallet.balance() < request.amount()) {
                    transferRepo.markFailed(transferId, "INSUFFICIENT_FUNDS");
                    result[0] = toJson(new TransferResponse(
                        transferId, "FAILED", "INSUFFICIENT_FUNDS",
                        request.fromWalletId(), request.toWalletId(), request.amount()));
                    return null; // transaction commits with FAILED status
                }

                // Steps 4–7: Update balances and insert double-entry ledger records
                walletRepo.debit(request.fromWalletId(), request.amount());
                walletRepo.credit(request.toWalletId(), request.amount());
                ledgerRepo.insertDebit(transferId, request.fromWalletId(), request.amount());
                ledgerRepo.insertCredit(transferId, request.toWalletId(), request.amount());

                // Step 8: Mark PROCESSED
                transferRepo.markProcessed(transferId);

                result[0] = toJson(new TransferResponse(
                    transferId, "PROCESSED", null,
                    request.fromWalletId(), request.toWalletId(), request.amount()));
                return null;
            });
        } catch (WalletNotFoundException e) {
            // Transaction rolled back (nothing inserted). Always return 200 — outcome is in the body.
            String body = toJson(new TransferResponse(
                null, "FAILED", "WALLET_NOT_FOUND",
                request.fromWalletId(), request.toWalletId(), request.amount()));
            idempotencyRepo.resolve(request.idempotencyKey(), 200, body);
            return ResponseEntity.ok().body(fromJson(body));
        }

        // Transaction committed — always 200, outcome is in the body
        String body = result[0];
        idempotencyRepo.resolve(request.idempotencyKey(), 200, body);
        return ResponseEntity.ok().body(fromJson(body));
    }

    private ResponseEntity<Object> pollForResult(String key) {
        for (int i = 0; i < 10; i++) {
            try {
                Thread.sleep(1000);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                break;
            }
            IdempotencyRepository.IdempotencyRecord record = idempotencyRepo.find(key);
            if (record != null && record.responseStatus() != 0) {
                return fromRecord(record);
            }
        }
        return ResponseEntity.status(503).body(
            Map.of("error", "Transfer still processing, please retry"));
    }

    private ResponseEntity<Object> fromRecord(IdempotencyRepository.IdempotencyRecord record) {
        return ResponseEntity.status(record.responseStatus()).body(fromJson(record.responseBody()));
    }

    private String toJson(Object obj) {
        try {
            return objectMapper.writeValueAsString(obj);
        } catch (Exception e) {
            throw new RuntimeException("JSON serialization failed", e);
        }
    }

    private Object fromJson(String json) {
        try {
            return objectMapper.readValue(json, Object.class);
        } catch (Exception e) {
            throw new RuntimeException("JSON deserialization failed", e);
        }
    }
}
