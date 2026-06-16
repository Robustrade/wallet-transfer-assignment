package com.viditec.wallettransfer.service;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.viditec.wallettransfer.api.request.CreateTransferRequest;
import com.viditec.wallettransfer.api.response.ErrorResponse;
import com.viditec.wallettransfer.api.response.TransferResponse;
import com.viditec.wallettransfer.api.response.WalletResponse;
import com.viditec.wallettransfer.exception.DuplicateIdempotencyKeyException;
import com.viditec.wallettransfer.domain.EntryType;
import com.viditec.wallettransfer.domain.TransferState;
import com.viditec.wallettransfer.repository.IdempotencyOperations;
import com.viditec.wallettransfer.repository.IdempotencyRecord;
import com.viditec.wallettransfer.repository.LedgerOperations;
import com.viditec.wallettransfer.repository.TransferOperations;
import com.viditec.wallettransfer.repository.WalletOperations;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.transaction.support.TransactionOperations;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.atomic.AtomicInteger;

import static org.assertj.core.api.Assertions.assertThat;

class TransferServiceTest {

    private InMemoryWalletOperations walletOperations;
    private InMemoryTransferOperations transferOperations;
    private InMemoryLedgerOperations ledgerOperations;
    private InMemoryIdempotencyOperations idempotencyOperations;
    private TransferService transferService;

    @BeforeEach
    void setUp() {
        walletOperations = new InMemoryWalletOperations();
        transferOperations = new InMemoryTransferOperations();
        ledgerOperations = new InMemoryLedgerOperations();
        idempotencyOperations = new InMemoryIdempotencyOperations();

        TransactionOperations transactionOperations = action -> action.doInTransaction(null);
        transferService = new TransferService(
                walletOperations,
                transferOperations,
                ledgerOperations,
                idempotencyOperations,
                transactionOperations,
                new ObjectMapper()
        );

        walletOperations.createWallet("wallet_1", 500);
        walletOperations.createWallet("wallet_2", 100);
        walletOperations.createWallet("wallet_3", 0);
    }

    @Test
    void createsTransferAndWritesBalancedLedger() {
        ResponseEntity<?> response = transferService.createTransfer(
                new CreateTransferRequest("idem-1", "wallet_1", "wallet_2", 100)
        );

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CREATED);
        assertThat(response.getBody()).isInstanceOf(TransferResponse.class);
        TransferResponse body = (TransferResponse) response.getBody();
        assertThat(body.state()).isEqualTo(TransferState.PROCESSED);
        assertThat(walletOperations.findById("wallet_1")).contains(new WalletResponse("wallet_1", 400));
        assertThat(walletOperations.findById("wallet_2")).contains(new WalletResponse("wallet_2", 200));
        assertThat(ledgerOperations.entries).hasSize(2);
        assertThat(ledgerOperations.entries.stream()
                .mapToLong(entry -> entry.entryType == EntryType.CREDIT ? entry.amount : -entry.amount)
                .sum()).isZero();
    }

    @Test
    void returnsOriginalResponseForDuplicateIdempotencyKey() {
        CreateTransferRequest request = new CreateTransferRequest("idem-2", "wallet_1", "wallet_2", 125);

        ResponseEntity<?> first = transferService.createTransfer(request);
        ResponseEntity<?> second = transferService.createTransfer(request);

        assertThat(first).usingRecursiveComparison().isEqualTo(second);
        assertThat(transferOperations.transfers).hasSize(1);
        assertThat(ledgerOperations.entries).hasSize(2);
    }

    @Test
    void rejectsIdempotencyKeyReuseWithDifferentPayload() {
        transferService.createTransfer(new CreateTransferRequest("idem-3", "wallet_1", "wallet_2", 100));

        ResponseEntity<?> duplicate = null;
        try {
            transferService.createTransfer(new CreateTransferRequest("idem-3", "wallet_1", "wallet_3", 100));
        } catch (DuplicateIdempotencyKeyException exception) {
            duplicate = ResponseEntity.status(HttpStatus.CONFLICT)
                    .body(new ErrorResponse(exception.getCode(), exception.getMessage()));
        }

        assertThat(duplicate).isNotNull();
        assertThat(duplicate.getStatusCode()).isEqualTo(HttpStatus.CONFLICT);
        assertThat(((ErrorResponse) duplicate.getBody()).code())
                .isEqualTo("IDEMPOTENCY_KEY_REUSED_WITH_DIFFERENT_PAYLOAD");
    }

    @Test
    void recordsFailureWithoutMovingMoneyWhenFundsAreInsufficient() {
        ResponseEntity<?> response = transferService.createTransfer(
                new CreateTransferRequest("idem-4", "wallet_2", "wallet_3", 200)
        );

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.UNPROCESSABLE_ENTITY);
        assertThat(response.getBody()).isInstanceOf(ErrorResponse.class);
        assertThat(walletOperations.findById("wallet_2")).contains(new WalletResponse("wallet_2", 100));
        assertThat(walletOperations.findById("wallet_3")).contains(new WalletResponse("wallet_3", 0));
        assertThat(ledgerOperations.entries).isEmpty();
        assertThat(transferOperations.lastState("idem-4")).isEqualTo(TransferState.FAILED);
    }

    @Test
    void preventsDoubleSpendingDuringConcurrentDebits() throws Exception {
        walletOperations.createWallet("wallet_4", 0);

        ExecutorService executorService = Executors.newFixedThreadPool(2);
        CountDownLatch ready = new CountDownLatch(2);
        CountDownLatch start = new CountDownLatch(1);

        Future<ResponseEntity<?>> first = executorService.submit(() ->
                sendConcurrentTransfer(ready, start, "idem-5a", "wallet_1", "wallet_3", 400)
        );
        Future<ResponseEntity<?>> second = executorService.submit(() ->
                sendConcurrentTransfer(ready, start, "idem-5b", "wallet_1", "wallet_4", 400)
        );

        ready.await();
        start.countDown();

        List<ResponseEntity<?>> responses = List.of(first.get(), second.get());
        executorService.shutdown();

        long createdCount = responses.stream().filter(r -> r.getStatusCode() == HttpStatus.CREATED).count();
        long failedCount = responses.stream()
                .filter(r -> r.getStatusCode() == HttpStatus.UNPROCESSABLE_ENTITY)
                .count();

        assertThat(createdCount).isEqualTo(1);
        assertThat(failedCount).isEqualTo(1);
        assertThat(walletOperations.findById("wallet_1")).contains(new WalletResponse("wallet_1", 100));

        long received = walletOperations.findById("wallet_3").orElseThrow().balance()
                + walletOperations.findById("wallet_4").orElseThrow().balance();
        assertThat(received).isEqualTo(400);
    }

    private ResponseEntity<?> sendConcurrentTransfer(
            CountDownLatch ready,
            CountDownLatch start,
            String idempotencyKey,
            String fromWalletId,
            String toWalletId,
            long amount
    ) throws Exception {
        ready.countDown();
        start.await();
        return transferService.createTransfer(new CreateTransferRequest(idempotencyKey, fromWalletId, toWalletId, amount));
    }

    private static final class InMemoryWalletOperations implements WalletOperations {

        private final Map<String, AtomicInteger> balances = new ConcurrentHashMap<>();

        @Override
        public void createWallet(String walletId, long initialBalance) {
            balances.put(walletId, new AtomicInteger(Math.toIntExact(initialBalance)));
        }

        @Override
        public Optional<WalletResponse> findById(String walletId) {
            AtomicInteger balance = balances.get(walletId);
            return balance == null ? Optional.empty() : Optional.of(new WalletResponse(walletId, balance.get()));
        }

        @Override
        public boolean exists(String walletId) {
            return balances.containsKey(walletId);
        }

        @Override
        public synchronized int debitIfSufficientFunds(String walletId, long amount) {
            AtomicInteger balance = balances.get(walletId);
            if (balance == null || balance.get() < amount) {
                return 0;
            }
            balance.addAndGet(-Math.toIntExact(amount));
            return 1;
        }

        @Override
        public synchronized int credit(String walletId, long amount) {
            AtomicInteger balance = balances.get(walletId);
            if (balance == null) {
                return 0;
            }
            balance.addAndGet(Math.toIntExact(amount));
            return 1;
        }
    }

    private static final class InMemoryTransferOperations implements TransferOperations {

        private final Map<String, TransferState> statesByIdempotencyKey = new ConcurrentHashMap<>();
        private final Map<String, String> transferIds = new ConcurrentHashMap<>();
        private final AtomicInteger counter = new AtomicInteger();
        private final List<String> transfers = new CopyOnWriteArrayList<>();

        @Override
        public String createPending(String idempotencyKey, String fromWalletId, String toWalletId, long amount) {
            String transferId = "trf_" + counter.incrementAndGet();
            transferIds.put(transferId, idempotencyKey);
            statesByIdempotencyKey.put(idempotencyKey, TransferState.PENDING);
            transfers.add(transferId);
            return transferId;
        }

        @Override
        public void markProcessed(String transferId) {
            statesByIdempotencyKey.put(transferIds.get(transferId), TransferState.PROCESSED);
        }

        @Override
        public void markFailed(String transferId, String failureReason) {
            statesByIdempotencyKey.put(transferIds.get(transferId), TransferState.FAILED);
        }

        private TransferState lastState(String idempotencyKey) {
            return statesByIdempotencyKey.get(idempotencyKey);
        }
    }

    private static final class InMemoryLedgerOperations implements LedgerOperations {

        private final List<LedgerEntry> entries = new CopyOnWriteArrayList<>();

        @Override
        public synchronized void createEntry(String transferId, String walletId, EntryType entryType, long amount) {
            entries.add(new LedgerEntry(transferId, walletId, entryType, amount));
        }
    }

    private record LedgerEntry(String transferId, String walletId, EntryType entryType, long amount) {
    }

    private static final class InMemoryIdempotencyOperations implements IdempotencyOperations {

        private final Map<String, IdempotencyRecord> records = new ConcurrentHashMap<>();

        @Override
        public synchronized boolean reserve(String idempotencyKey, String requestHash) {
            return records.putIfAbsent(
                    idempotencyKey,
                    new IdempotencyRecord(idempotencyKey, requestHash, null, null, null)
            ) == null;
        }

        @Override
        public Optional<IdempotencyRecord> findByKey(String idempotencyKey) {
            return Optional.ofNullable(records.get(idempotencyKey));
        }

        @Override
        public synchronized void complete(String idempotencyKey, String transferId, int httpStatus, String responseBody) {
            IdempotencyRecord record = records.get(idempotencyKey);
            records.put(
                    idempotencyKey,
                    new IdempotencyRecord(
                            idempotencyKey,
                            record.requestHash(),
                            transferId,
                            httpStatus,
                            responseBody
                    )
            );
        }
    }
}
