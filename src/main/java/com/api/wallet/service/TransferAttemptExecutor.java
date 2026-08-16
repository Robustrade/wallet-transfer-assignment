package com.api.wallet.service;

import com.api.wallet.domain.IdempotencyRecord;
import com.api.wallet.domain.LedgerEntry;
import com.api.wallet.domain.Transfer;
import com.api.wallet.domain.Wallet;
import com.api.wallet.dto.CreateTransferRequest;
import com.api.wallet.dto.TransferResponse;
import com.api.wallet.exception.WalletNotFoundException;
import com.api.wallet.repository.IdempotencyRecordRepository;
import com.api.wallet.repository.LedgerEntryRepository;
import com.api.wallet.repository.TransferRepository;
import com.api.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import tools.jackson.databind.json.JsonMapper;

@Service
class TransferAttemptExecutor {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final JsonMapper jsonMapper;

    TransferAttemptExecutor(
            WalletRepository walletRepository,
            TransferRepository transferRepository,
            LedgerEntryRepository ledgerEntryRepository,
            IdempotencyRecordRepository idempotencyRecordRepository,
            JsonMapper jsonMapper) {
        this.walletRepository = walletRepository;
        this.transferRepository = transferRepository;
        this.ledgerEntryRepository = ledgerEntryRepository;
        this.idempotencyRecordRepository = idempotencyRecordRepository;
        this.jsonMapper = jsonMapper;
    }

    @Transactional
    TransferResponse attemptTransfer(CreateTransferRequest request) {
        String fromWalletId = request.fromWalletId();
        String toWalletId = request.toWalletId();
        BigDecimal amount = request.amount();

        if (!walletRepository.existsById(fromWalletId)) {
            throw new WalletNotFoundException(fromWalletId);
        }
        if (!walletRepository.existsById(toWalletId)) {
            throw new WalletNotFoundException(toWalletId);
        }

        Wallet fromWallet;
        Wallet toWallet;
        if (fromWalletId.compareTo(toWalletId) < 0) {
            fromWallet = lockWallet(fromWalletId);
            toWallet = lockWallet(toWalletId);
        } else {
            toWallet = lockWallet(toWalletId);
            fromWallet = lockWallet(fromWalletId);
        }

        Transfer transfer = Transfer.initiate(request.idempotencyKey(), fromWalletId, toWalletId, amount);
        transferRepository.saveAndFlush(transfer);

        TransferResponse response = process(transfer, fromWallet, toWallet, amount);

        idempotencyRecordRepository.save(
                IdempotencyRecord.of(
                        request.idempotencyKey(),
                        transfer.getId(),
                        transfer.getStatus().httpStatus(),
                        writeJson(response)));

        return response;
    }

    private Wallet lockWallet(String walletId) {
        return walletRepository.findByIdForUpdate(walletId).orElseThrow(() -> new WalletNotFoundException(walletId));
    }

    private TransferResponse process(Transfer transfer, Wallet fromWallet, Wallet toWallet, BigDecimal amount) {
        if (fromWallet.getBalance().compareTo(amount) < 0) {
            transfer.markFailed("INSUFFICIENT_FUNDS");
            transferRepository.save(transfer);
            return TransferResponse.from(transfer);
        }

        fromWallet.debit(amount);
        toWallet.credit(amount);
        ledgerEntryRepository.save(LedgerEntry.debit(transfer.getId(), fromWallet.getId(), amount));
        ledgerEntryRepository.save(LedgerEntry.credit(transfer.getId(), toWallet.getId(), amount));

        transfer.markProcessed();
        transferRepository.save(transfer);
        return TransferResponse.from(transfer);
    }

    private String writeJson(TransferResponse response) {
        return jsonMapper.writeValueAsString(response);
    }
}
