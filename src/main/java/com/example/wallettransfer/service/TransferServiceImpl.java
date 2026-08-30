package com.example.wallettransfer.service;

import com.example.wallettransfer.domain.IdempotencyRecord;
import com.example.wallettransfer.domain.LedgerEntry;
import com.example.wallettransfer.domain.Transfer;
import com.example.wallettransfer.domain.Wallet;
import com.example.wallettransfer.dto.CreateTransferRequest;
import com.example.wallettransfer.dto.TransferResponse;
import com.example.wallettransfer.exception.IdempotencyConflictException;
import com.example.wallettransfer.exception.InvalidTransferException;
import com.example.wallettransfer.exception.WalletNotFoundException;
import com.example.wallettransfer.repository.IdempotencyLockRepository;
import com.example.wallettransfer.repository.IdempotencyRecordRepository;
import com.example.wallettransfer.repository.LedgerEntryRepository;
import com.example.wallettransfer.repository.TransferRepository;
import com.example.wallettransfer.repository.WalletRepository;
import com.example.wallettransfer.util.RequestHashGenerator;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import java.math.BigDecimal;
import java.math.RoundingMode;
import java.util.List;

@Service
@RequiredArgsConstructor
@Slf4j
public class TransferServiceImpl implements TransferService {

    private static final String INSUFFICIENT_FUNDS = "INSUFFICIENT_FUNDS";

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final IdempotencyLockRepository idempotencyLockRepository;


    /**
     * The main entry point for moving money between two wallets.
     * This whole thing runs in a single DB transaction — so either everything
     * commits (balances updated, ledger written, transfer recorded) or nothing does.
     *
     * We also handle idempotency here: if the same key comes in again with the
     * same payload, we just return the original result without touching anything.
     * If the key is reused with a different payload, we throw a conflict.
     */
    @Transactional
    @Override
    public TransferResponse createTransfer(CreateTransferRequest request) {
        validateDifferentWallets(request);

        String requestHash = RequestHashGenerator.hash(request);
        BigDecimal amount = request.amount().setScale(4, RoundingMode.UNNECESSARY);
        idempotencyLockRepository.lock(request.idempotencyKey());

        var existing = idempotencyRecordRepository.findWithTransferByKey(request.idempotencyKey());
        if (existing.isPresent()) {
            IdempotencyRecord record = existing.get();
            if (!record.getRequestHash().equals(requestHash)) {
                throw new IdempotencyConflictException(request.idempotencyKey());
            }
            log.info("Replaying transfer {} for idempotency key {}",
                    record.getTransfer().getId(), request.idempotencyKey());
            return toResponse(record.getTransfer(), record.getIdempotencyKey());
        }

        WalletPair wallets = lockWalletsInStableOrder(request.fromWalletId(), request.toWalletId());
        // Flush the PENDING transfer before creating the idempotency record, whose FK references it.
        Transfer transfer = transferRepository.saveAndFlush(Transfer.pending(
                request.fromWalletId(),
                request.toWalletId(),
                amount
        ));
        idempotencyRecordRepository.save(new IdempotencyRecord(
                request.idempotencyKey(),
                requestHash,
                transfer
        ));

        Wallet source = wallets.source();
        Wallet destination = wallets.destination();

        if (!source.hasSufficientBalance(amount)) {
            transfer.markFailed(INSUFFICIENT_FUNDS);
            log.info("Transfer {} failed because source wallet {} has insufficient funds",
                    transfer.getId(), source.getId());
            return toResponse(transfer, request.idempotencyKey());
        }

        source.debit(amount);
        destination.credit(amount);

        ledgerEntryRepository.saveAll(List.of(
                LedgerEntry.debit(transfer.getId(), source.getId(), amount),
                LedgerEntry.credit(transfer.getId(), destination.getId(), amount)
        ));

        transfer.markProcessed();
        log.info("Transfer {} processed from wallet {} to wallet {}",
                transfer.getId(), source.getId(), destination.getId());
        return toResponse(transfer, request.idempotencyKey());
    }

    // Quick sanity check — no point locking wallets or hitting the DB
    // if someone is trying to transfer money to themselves.
    private void validateDifferentWallets(CreateTransferRequest request) {
        if (request.fromWalletId().equals(request.toWalletId())) {
            throw new InvalidTransferException("Source and destination wallets must be different");
        }
    }

    /**
     * Fetches both wallets and locks their rows for the duration of this transaction.
     *
     * The trick here is locking in consistent lexical order (lower ID first) regardless
     * of which wallet is source and which is destination. Without this, two concurrent
     * transfers going in opposite directions (A->B and B->A) could deadlock each other.
     */
    private WalletPair lockWalletsInStableOrder(String sourceId, String destinationId) {
        String firstId = sourceId.compareTo(destinationId) < 0 ? sourceId : destinationId;
        String secondId = sourceId.compareTo(destinationId) < 0 ? destinationId : sourceId;

        Wallet first = walletRepository.findByIdForUpdate(firstId)
                .orElseThrow(() -> new WalletNotFoundException(firstId));
        Wallet second = walletRepository.findByIdForUpdate(secondId)
                .orElseThrow(() -> new WalletNotFoundException(secondId));

        Wallet source = first.getId().equals(sourceId) ? first : second;
        Wallet destination = first.getId().equals(destinationId) ? first : second;
        return new WalletPair(source, destination);
    }

    // Just maps the Transfer domain object to what the API caller gets back.
    // Keeping this separate so the service logic above stays readable.
    private TransferResponse toResponse(Transfer transfer, String idempotencyKey) {
        return new TransferResponse(
                transfer.getId(),
                idempotencyKey,
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount(),
                transfer.getStatus(),
                transfer.getFailureReason(),
                transfer.getCreatedAt(),
                transfer.getCompletedAt()
        );
    }

    private record WalletPair(Wallet source, Wallet destination) {
    }
}
