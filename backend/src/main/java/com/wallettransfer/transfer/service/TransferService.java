package com.wallettransfer.transfer.service;

import com.wallettransfer.idempotency.domain.IdempotencyRecord;
import com.wallettransfer.idempotency.service.IdempotencyService;
import com.wallettransfer.idempotency.service.IdempotencyService.CachedTransferResult;
import com.wallettransfer.idempotency.service.IdempotencyService.IdempotencyReservation;
import com.wallettransfer.ledger.service.LedgerService;
import com.wallettransfer.transfer.domain.Transfer;
import com.wallettransfer.transfer.domain.TransferStatus;
import com.wallettransfer.transfer.dto.CreateTransferRequest;
import com.wallettransfer.transfer.dto.TransferResponse;
import com.wallettransfer.transfer.repository.TransferRepository;
import com.wallettransfer.wallet.domain.InsufficientBalanceException;
import com.wallettransfer.wallet.service.WalletService;
import java.util.List;
import java.util.Optional;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class TransferService {

    private final TransferRepository transferRepository;
    private final WalletService walletService;
    private final LedgerService ledgerService;
    private final IdempotencyService idempotencyService;

    public TransferService(
            TransferRepository transferRepository,
            WalletService walletService,
            LedgerService ledgerService,
            IdempotencyService idempotencyService) {
        this.transferRepository = transferRepository;
        this.walletService = walletService;
        this.ledgerService = ledgerService;
        this.idempotencyService = idempotencyService;
    }

    @Transactional(readOnly = true)
    public List<TransferResponse> listTransfers() {
        return transferRepository.findAllByOrderByCreatedAtDesc().stream()
                .map(transfer -> toResponse(transfer, null, false))
                .toList();
    }

    @Transactional(noRollbackFor = InsufficientBalanceException.class)
    public TransferResult createTransfer(CreateTransferRequest request) {
        validateRequest(request);

        Optional<IdempotencyRecord> existingRecord = idempotencyService.findByKey(request.idempotencyKey());
        if (existingRecord.isPresent()) {
            return completeFromRecord(existingRecord.get(), request.idempotencyKey(), true);
        }

        walletService.ensureWalletsExist(request.fromWalletId(), request.toWalletId());

        Transfer transfer = transferRepository.save(
                new Transfer(request.fromWalletId(), request.toWalletId(), request.amount()));

        IdempotencyReservation reservation = idempotencyService.reserve(request.idempotencyKey(), transfer.getId());
        if (reservation.duplicate()) {
            transfer = transferRepository
                    .findById(reservation.record().getTransferId())
                    .orElseThrow(() -> new IllegalStateException("Transfer missing for idempotency key"));
            return completeFromRecord(reservation.record(), request.idempotencyKey(), true);
        }

        return executeTransfer(transfer, reservation.record(), request.idempotencyKey(), false);
    }

    private TransferResult completeFromRecord(
            IdempotencyRecord record, String idempotencyKey, boolean duplicate) {
        Optional<CachedTransferResult> cached = idempotencyService.getCachedResult(record, duplicate);
        if (cached.isPresent()) {
            CachedTransferResult result = cached.get();
            return new TransferResult(result.response(), result.httpStatus(), result.duplicate());
        }

        Transfer transfer = transferRepository
                .findById(record.getTransferId())
                .orElseThrow(() -> new IllegalStateException("Transfer missing for idempotency key"));

        return executeTransfer(transfer, record, idempotencyKey, duplicate);
    }

    private TransferResult executeTransfer(
            Transfer transfer, IdempotencyRecord record, String idempotencyKey, boolean duplicate) {
        if (transfer.getStatus() == TransferStatus.PENDING) {
            try {
                processPendingTransfer(transfer);
            } catch (InsufficientBalanceException ex) {
                transfer.markFailed(ex.getMessage());
                transferRepository.save(transfer);
            }
        }

        TransferResponse response = toResponse(transfer, idempotencyKey, duplicate);
        int httpStatus = transfer.getStatus() == TransferStatus.FAILED ? 422 : 200;
        idempotencyService.storeResponse(record, response, httpStatus);
        return new TransferResult(response, httpStatus, duplicate);
    }

    private void processPendingTransfer(Transfer transfer) {
        walletService.moveFunds(transfer.getFromWalletId(), transfer.getToWalletId(), transfer.getAmount());
        ledgerService.recordTransfer(transfer);
        transfer.markProcessed();
        transferRepository.save(transfer);
    }

    private void validateRequest(CreateTransferRequest request) {
        if (request.fromWalletId().equals(request.toWalletId())) {
            throw new IllegalArgumentException("Source and destination wallet must be different");
        }
    }

    private TransferResponse toResponse(Transfer transfer, String idempotencyKey, boolean duplicate) {
        return new TransferResponse(
                transfer.getId(),
                idempotencyKey,
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount(),
                transfer.getStatus(),
                transfer.getFailureReason(),
                duplicate,
                transfer.getCreatedAt());
    }

    public record TransferResult(TransferResponse response, int httpStatus, boolean duplicate) {
    }
}
