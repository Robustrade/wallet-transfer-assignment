package com.kullu.wallet.service;

import java.util.Comparator;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kullu.wallet.dto.request.CreateTransferRequest;
import com.kullu.wallet.dto.response.IdempotencyResponse;
import com.kullu.wallet.dto.response.TransferOutcome;
import com.kullu.wallet.dto.response.TransferResponse;
import com.kullu.wallet.entity.EntryType;
import com.kullu.wallet.entity.LedgerEntry;
import com.kullu.wallet.entity.TransferEntity;
import com.kullu.wallet.entity.WalletEntity;
import com.kullu.wallet.exception.IdempotencyConflictException;
import com.kullu.wallet.exception.SelfTransferException;
import com.kullu.wallet.repository.TransferRepository;

/**
 * Orchestrates wallet-to-wallet transfers with idempotent semantics, safe
 * concurrent execution, and a double-entry ledger.
 *
 * <p>All side effects of a single API call execute inside one DB transaction:
 * insertion of the transfer row, acquisition of pessimistic row locks on both
 * wallets, balance updates, ledger inserts, transfer status transition, and
 * finalisation of the idempotency record. If anything fails, the transaction
 * rolls back and the idempotency claim disappears with it.
 *
 * <p>The default Spring propagation is REQUIRED, and the default Postgres
 * isolation is READ_COMMITTED. That is enough here because wallet writes are
 * serialized explicitly with row-level locks.
 */
@Service
public class TransferService {

    private static final Logger log = LoggerFactory.getLogger(TransferService.class);

    private final TransferRepository transfers;
    private final WalletService walletService;
    private final LedgerService ledgerService;
    private final IdempotencyService idempotencyService;
    private final ObjectMapper objectMapper;

    public TransferService(final TransferRepository transfers,
                           final WalletService walletService,
                           final LedgerService ledgerService,
                           final IdempotencyService idempotencyService,
                           final ObjectMapper objectMapper) {
        this.transfers = transfers;
        this.walletService = walletService;
        this.ledgerService = ledgerService;
        this.idempotencyService = idempotencyService;
        this.objectMapper = objectMapper;
    }

    @Transactional
    public TransferOutcome createTransfer(final CreateTransferRequest request) {
        validateTransferRequest(request);

        String requestHash = requestHashFor(request);
        Optional<IdempotencyResponse> existing = idempotencyService.findByKey(request.getIdempotencyKey());
        if (existing.isPresent()) {
            return replay(existing.get(), requestHash);
        }

        return createNewTransfer(request, requestHash);
    }

    public TransferOutcome replay(final IdempotencyResponse existing, final String requestHash) {
        if (!existing.requestHash().equals(requestHash)) {
            throw new IdempotencyConflictException(
                IdempotencyConflictException.Kind.PAYLOAD_MISMATCH,
                "idempotency key reused with a different request payload");
        }
        if (!existing.hasResponse()) {
            throw new IdempotencyConflictException(
                IdempotencyConflictException.Kind.IN_FLIGHT,
                "duplicate request for idempotency key is still in flight");
        }
        return new TransferOutcome(true, existing.responseStatus(), existing.responseBody());
    }

    private void validateTransferRequest(final CreateTransferRequest request) {
        if (request.getFromWalletId().equals(request.getToWalletId())) {
            throw new SelfTransferException();
        }
    }

    private String requestHashFor(final CreateTransferRequest request) {
        return RequestHasher.hash(
            request.getFromWalletId(), request.getToWalletId(), request.getAmount());
    }

    private TransferOutcome createNewTransfer(final CreateTransferRequest request, final String requestHash) {
        LockedWallets wallets = lockWallets(request.getFromWalletId(), request.getToWalletId());
        TransferEntity transferEntity = createPendingTransfer(wallets, request.getAmount());
        claimIdempotencyKey(request.getIdempotencyKey(), requestHash, transferEntity.getId());

        executeTransfer(wallets, transferEntity);
        return finalizeOutcome(request.getIdempotencyKey(), transferEntity);
    }

    private LockedWallets lockWallets(final String fromWalletId, final String toWalletId) {
        List<String> ordered = List.of(fromWalletId, toWalletId)
            .stream()
            .sorted(Comparator.naturalOrder())
            .toList();

        WalletEntity first = walletService.lockAndGet(ordered.get(0));
        WalletEntity second = walletService.lockAndGet(ordered.get(1));

        WalletEntity from = first.getId().equals(fromWalletId) ? first : second;
        WalletEntity to = first.getId().equals(toWalletId) ? first : second;
        return new LockedWallets(from, to);
    }

    private TransferEntity createPendingTransfer(final LockedWallets wallets, final long amount) {
        TransferEntity transferEntity = new TransferEntity(
            UUID.randomUUID(), wallets.from().getId(), wallets.to().getId(), amount);
        return saveTransfer(transferEntity);
    }

    private void claimIdempotencyKey(final String idempotencyKey,
                                     final String requestHash,
                                     final UUID transferId) {
        int claimed = idempotencyService.tryClaim(idempotencyKey, requestHash, transferId);
        if (claimed == 0) {
            log.debug("idempotency key {} taken by another request", idempotencyKey);
            throw new IdempotencyConflictException(
                IdempotencyConflictException.Kind.IN_FLIGHT,
                "duplicate request for idempotency key is still in flight");
        }

    }

    private void executeTransfer(final LockedWallets wallets, final TransferEntity transferEntity) {
        WalletEntity from = wallets.from();
        WalletEntity to = wallets.to();

        if (hasInsufficientFunds(from, transferEntity)) {
            transferEntity.markFailed("INSUFFICIENT_FUNDS");
            saveTransfer(transferEntity);
            return;
        }

        applySuccessfulTransfer(from, to, transferEntity);
    }

    private boolean hasInsufficientFunds(final WalletEntity from, final TransferEntity transferEntity) {
        return from.getBalance() < transferEntity.getAmount();
    }

    private void applySuccessfulTransfer(final WalletEntity from, final WalletEntity to, final TransferEntity transferEntity) {
        from.debit(transferEntity.getAmount());
        to.credit(transferEntity.getAmount());
        walletService.save(from);
        walletService.save(to);

        ledgerService.save(new LedgerEntry(
            from.getId(), transferEntity.getId(), EntryType.DEBIT, transferEntity.getAmount()));
        ledgerService.save(new LedgerEntry(
            to.getId(), transferEntity.getId(), EntryType.CREDIT, transferEntity.getAmount()));

        transferEntity.markProcessed();
        saveTransfer(transferEntity);
    }

    private TransferOutcome finalizeOutcome(final String idempotencyKey, final TransferEntity transferEntity) {
        TransferResponse response = new TransferResponse(
            transferEntity.getId(),
            transferEntity.getStatus(),
            transferEntity.getFromWalletId(),
            transferEntity.getToWalletId(),
            transferEntity.getAmount(),
            transferEntity.getFailureReason(),
            transferEntity.getCreatedAt());
        short httpStatus = (short) successStatusFor(transferEntity).value();
        String body = serialize(response);
        idempotencyService.storeResponse(idempotencyKey, httpStatus, body);

        return new TransferOutcome(false, httpStatus, body);
    }

    private TransferEntity saveTransfer(final TransferEntity transferEntity) {
        return transfers.saveAndFlush(transferEntity);
    }

    private static HttpStatus successStatusFor(final TransferEntity transferEntity) {
        return switch (transferEntity.getStatus()) {
            case PROCESSED -> HttpStatus.CREATED;
            case FAILED -> HttpStatus.UNPROCESSABLE_ENTITY;
            case PENDING -> throw new IllegalStateException(
                "transfer must be in terminal state, was PENDING");
        };
    }

    private String serialize(final TransferResponse response) {
        try {
            return objectMapper.writeValueAsString(response);
        } catch (JsonProcessingException e) {
            throw new IllegalStateException("failed to serialise transfer response", e);
        }
    }

    private record LockedWallets(WalletEntity from, WalletEntity to) {
    }
}
