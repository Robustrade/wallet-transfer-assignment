package com.example.wallet.service.impl;

import com.example.wallet.exception.InsufficientFundsException;
import com.example.wallet.exception.WalletNotFoundException;
import com.example.wallet.handler.dto.CreateTransferRequest;
import com.example.wallet.handler.dto.TransferResponse;
import com.example.wallet.model.Transfer;
import com.example.wallet.model.TransferStatus;
import com.example.wallet.model.Wallet;
import com.example.wallet.repository.IdempotencyRepository;
import com.example.wallet.repository.LedgerRepository;
import com.example.wallet.repository.TransferRepository;
import com.example.wallet.repository.WalletRepository;
import com.example.wallet.service.TransferMetrics;
import com.example.wallet.service.TransferService;
import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.micrometer.core.instrument.Timer;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;
import org.springframework.transaction.support.TransactionTemplate;

import java.time.Duration;
import java.time.Instant;
import java.util.List;
import java.util.Optional;

@Slf4j
@Service
@RequiredArgsConstructor
public class TransferServiceImpl implements TransferService {

    public static final String ENDPOINT = "POST /transfers";

    private final TransferRepository transferRepository;
    private final WalletRepository walletRepository;
    private final LedgerRepository ledgerRepository;
    private final IdempotencyRepository idempotencyRepository;
    private final TransactionTemplate transactionTemplate;
    private final ObjectMapper objectMapper;
    private final TransferMetrics metrics;

    @Value("${wallet.idempotency.ttl-hours:24}")
    private int idempotencyTtlHours;

    @Override
    public TransferResponse execute(CreateTransferRequest request) {
        Timer.Sample sample = metrics.startTimer();
        try {
            return doExecute(request, sample);
        } catch (InsufficientFundsException e) {
            // FAILED outcome — funds check failed; FAILED state already committed in DB
            metrics.recordFailed(sample, request.amount());
            throw e;
        } catch (Exception e) {
            metrics.recordError(sample);
            throw e;
        }
    }

    private TransferResponse doExecute(CreateTransferRequest request, Timer.Sample sample) {

        // ── Fast-path (outside transaction) ──────────────────────────────────────
        // Performance optimisation: avoids opening a transaction for the common
        // replay case. Safe to read outside a transaction because idempotency_records
        // only ever contains TERMINAL state entries (PROCESSED / FAILED). Terminal
        // states are immutable — once written they never change — so a read here
        // can never return stale "PENDING then later PROCESSED" data.
        //
        // TOCTOU gap: between this read and entering the transaction below, another
        // thread could commit the same key. This is handled by the in-transaction
        // re-check (Step 2) which closes the gap before any side effects are applied.
        Optional<TransferResponse> cached = idempotencyRepository
                .find(request.idempotencyKey(), ENDPOINT)
                .map(record -> deserialize(record.responseBody()));

        if (cached.isPresent()) {
            log.debug("Idempotent replay (fast-path) key={}", request.idempotencyKey());
            metrics.recordReplay(sample, "fast_path");
            return replayOrThrow(cached.get(), request.fromWalletId());
        }

        // ── Full workflow (inside transaction) ────────────────────────────────────
        // The idempotency record is saved INSIDE the same transaction so that:
        //   • rollback → record not stored → next retry starts fresh
        //   • commit   → record atomically visible → replay is deterministic
        TransferResponse response = transactionTemplate.execute(txStatus -> {

            // Step 2: In-transaction idempotency re-check — closes the TOCTOU gap.
            // If another thread committed the same key between Step 1 and here,
            // we return their committed result without executing any side effects.
            // This read is inside the transaction so it sees the latest committed data.
            Optional<TransferResponse> innerCached = idempotencyRepository
                    .find(request.idempotencyKey(), ENDPOINT)
                    .map(record -> deserialize(record.responseBody()));

            if (innerCached.isPresent()) {
                log.debug("Idempotent replay (in-tx re-check) key={}", request.idempotencyKey());
                metrics.recordReplay(sample, "in_tx");
                return innerCached.get();
            }

            // Step 3: Lock wallets in sorted order BEFORE inserting the transfer.
            //
            // Root cause of FK-check deadlock (original order: INSERT then FOR UPDATE):
            //   PostgreSQL FK validation acquires FOR KEY SHARE on referenced wallet rows.
            //   FOR KEY SHARE conflicts with FOR UPDATE. Two concurrent threads each hold
            //   FOR KEY SHARE on the same wallet and each wait for FOR UPDATE → circular
            //   wait → deadlock detected by PostgreSQL.
            //
            // Fix: acquire FOR UPDATE first. FOR UPDATE subsumes FOR KEY SHARE, so our
            //   own FK check (in the INSERT below) is granted immediately. Every other
            //   thread blocks at this lockForUpdate call rather than inside the INSERT,
            //   eliminating the circular dependency entirely.
            //
            // Bonus: validating wallet existence here (before the INSERT) means a
            //   missing wallet throws WalletNotFoundException instead of a FK-violation
            //   DataIntegrityViolationException, giving callers the correct 404 response.
            List<String> sortedIds = List.of(request.fromWalletId(), request.toWalletId())
                    .stream().sorted().toList();

            List<Wallet> lockedWallets = walletRepository.lockForUpdate(sortedIds);

            Wallet fromWallet = lockedWallets.stream()
                    .filter(w -> w.id().equals(request.fromWalletId()))
                    .findFirst()
                    .orElseThrow(() -> new WalletNotFoundException(request.fromWalletId()));

            lockedWallets.stream()
                    .filter(w -> w.id().equals(request.toWalletId()))
                    .findFirst()
                    .orElseThrow(() -> new WalletNotFoundException(request.toWalletId()));

            // Step 4: Insert transfer in PENDING — safe now: wallets are validated and locked.
            // ON CONFLICT DO NOTHING handles threads that race past both idempotency checks
            // simultaneously. The second thread's INSERT waits on the unique-index lock for
            // idempotency_key, hits DO NOTHING after the first tx commits, then re-reads the
            // committed terminal state below.
            Transfer transfer = transferRepository.insertPending(request);

            if (transfer.status() != TransferStatus.PENDING) {
                // Idempotent replay via on-conflict path — wallets locked unnecessarily but
                // correctness is maintained; no balance changes were applied.
                log.debug("Transfer reached terminal state before processing transferId={}", transfer.id());
                metrics.recordReplay(sample, "on_conflict");
                return TransferResponse.from(transfer, false);
            }

            // Step 5: Balance check → FAILED (no ledger entries written)
            if (fromWallet.balance().compareTo(request.amount()) < 0) {
                log.warn("Insufficient funds walletId={} balance={} requested={}",
                        fromWallet.id(), fromWallet.balance(), request.amount());
                transferRepository.updateStatus(transfer.id(), TransferStatus.FAILED);
                TransferResponse failedResponse = TransferResponse.from(
                        transfer.withStatus(TransferStatus.FAILED), true);
                saveIdempotencyRecord(request.idempotencyKey(), failedResponse, HttpStatus.UNPROCESSABLE_ENTITY);
                return failedResponse;
            }

            // Step 6: Debit source, credit destination
            walletRepository.debit(request.fromWalletId(), request.amount());
            walletRepository.credit(request.toWalletId(), request.amount());

            // Step 7: Double-entry ledger
            ledgerRepository.insertDebit(transfer.id(), request.fromWalletId(), request.amount());
            ledgerRepository.insertCredit(transfer.id(), request.toWalletId(), request.amount());

            // Step 8: Mark PROCESSED and persist idempotency record atomically
            transferRepository.updateStatus(transfer.id(), TransferStatus.PROCESSED);

            log.info("Transfer completed transferId={} from={} to={} amount={}",
                    transfer.id(), request.fromWalletId(), request.toWalletId(), request.amount());

            TransferResponse processedResponse = TransferResponse.from(
                    transfer.withStatus(TransferStatus.PROCESSED), true);

            saveIdempotencyRecord(request.idempotencyKey(), processedResponse, HttpStatus.CREATED);

            return processedResponse;
        });

        if (response == null) {
            throw new IllegalStateException("Transaction returned null — this should never happen");
        }

        // Defensive: we must never surface a PENDING response to the caller.
        // PENDING is an internal state — callers only ever receive PROCESSED (201/200)
        // or an InsufficientFundsException (422).
        if (response.status() == TransferStatus.PENDING) {
            throw new IllegalStateException(
                    "Invariant violation: PENDING response reached the caller for key="
                            + request.idempotencyKey());
        }

        if (response.status() == TransferStatus.FAILED) {
            throw new InsufficientFundsException(request.fromWalletId());
        }

        metrics.recordProcessed(sample, request.amount());
        return response;
    }

    // ── Private helpers ───────────────────────────────────────────────────────────

    /** Replay a cached response or throw for FAILED transfers — used by both fast-paths. */
    private TransferResponse replayOrThrow(TransferResponse cached, String fromWalletId) {
        if (cached.status() == TransferStatus.FAILED) {
            throw new InsufficientFundsException(fromWalletId);
        }
        return cached;
    }

    /**
     * Serialize and persist the idempotency record inside the current transaction.
     * newlyCreated is excluded from serialization (@JsonProperty WRITE_ONLY) so the
     * stored JSON naturally deserializes with newlyCreated=false on replay — correct behaviour.
     */
    private void saveIdempotencyRecord(String key, TransferResponse response, HttpStatus status) {
        Instant expiresAt = Instant.now().plus(Duration.ofHours(idempotencyTtlHours));
        idempotencyRepository.save(key, ENDPOINT, serialize(response), status.value(), expiresAt);
    }

    private String serialize(TransferResponse response) {
        try {
            return objectMapper.writeValueAsString(response);
        } catch (JsonProcessingException e) {
            throw new IllegalStateException("Failed to serialize TransferResponse for idempotency record", e);
        }
    }

    private TransferResponse deserialize(String json) {
        try {
            return objectMapper.readValue(json, TransferResponse.class);
        } catch (JsonProcessingException e) {
            throw new IllegalStateException("Failed to deserialize TransferResponse from idempotency record", e);
        }
    }
}
