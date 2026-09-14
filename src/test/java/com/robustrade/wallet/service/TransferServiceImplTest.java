package com.robustrade.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.lenient;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.robustrade.wallet.domain.IdempotencyRecord;
import com.robustrade.wallet.domain.LedgerEntry;
import com.robustrade.wallet.domain.Transfer;
import com.robustrade.wallet.domain.TransferState;
import com.robustrade.wallet.domain.Wallet;
import com.robustrade.wallet.handler.dto.CreateTransferRequest;
import com.robustrade.wallet.handler.dto.TransferResponse;
import com.robustrade.wallet.metrics.TransferMetrics;
import com.robustrade.wallet.repository.IdempotencyRecordRepository;
import com.robustrade.wallet.repository.LedgerEntryRepository;
import com.robustrade.wallet.repository.TransferRepository;
import com.robustrade.wallet.repository.WalletRepository;
import io.micrometer.observation.ObservationRegistry;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.Optional;
import java.util.UUID;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.transaction.TransactionStatus;
import org.springframework.transaction.support.TransactionCallback;
import org.springframework.transaction.support.TransactionTemplate;

@ExtendWith(MockitoExtension.class)
class TransferServiceImplTest {

    @Mock
    private WalletRepository walletRepository;
    @Mock
    private TransferRepository transferRepository;
    @Mock
    private LedgerEntryRepository ledgerEntryRepository;
    @Mock
    private IdempotencyRecordRepository idempotencyRecordRepository;
    @Mock
    private TransferStatusService transferStatusService;
    @Mock
    private TransactionTemplate transactionTemplate;
    @Mock
    private TransferMetrics transferMetrics;

    private TransferServiceImpl transferService;

    private final AtomicReference<Transfer> storedTransfer = new AtomicReference<>();

    @BeforeEach
    void setUp() {
        transferService = new TransferServiceImpl(
                walletRepository,
                transferRepository,
                ledgerEntryRepository,
                idempotencyRecordRepository,
                transferStatusService,
                transactionTemplate,
                transferMetrics,
                ObservationRegistry.NOOP
        );
        // Shared stub: some tests exit before any transactional callback runs.
        lenient().when(transactionTemplate.execute(any())).thenAnswer(invocation -> {
            TransactionCallback<?> callback = invocation.getArgument(0);
            return callback.doInTransaction(org.mockito.Mockito.mock(TransactionStatus.class));
        });
        lenient().when(transferMetrics.track(any())).thenAnswer(invocation -> {
            java.util.function.Supplier<?> supplier = invocation.getArgument(0);
            return supplier.get();
        });
    }

    @Test
    void createTransfer_whenNewRequest_completesSuccessfully() {
        CreateTransferRequest request = request("key-1", "wallet-a", "wallet-b", "100.00");
        Wallet from = wallet("wallet-a", "500.00");
        Wallet to = wallet("wallet-b", "50.00");

        when(walletRepository.existsById("wallet-a")).thenReturn(true);
        when(walletRepository.existsById("wallet-b")).thenReturn(true);
        when(idempotencyRecordRepository.findByIdempotencyKey("key-1")).thenReturn(Optional.empty());
        stubPersistence(from, to);

        TransferResponse response = transferService.createTransfer(request);

        assertThat(response.getState()).isEqualTo(TransferState.PROCESSED);
        assertThat(response.getIdempotencyKey()).isEqualTo("key-1");
        assertThat(response.getFromWalletId()).isEqualTo("wallet-a");
        assertThat(response.getToWalletId()).isEqualTo("wallet-b");
        assertThat(response.getAmount()).isEqualByComparingTo("100.00");
        assertThat(response.getFailureReason()).isNull();
        assertThat(from.getBalance()).isEqualByComparingTo("400.00");
        assertThat(to.getBalance()).isEqualByComparingTo("150.00");

        verify(transferRepository, atLeastOnce()).save(any(Transfer.class));
        verify(idempotencyRecordRepository).save(any(IdempotencyRecord.class));
        verify(ledgerEntryRepository, times(2)).save(any(LedgerEntry.class));
        verify(walletRepository, times(2)).save(any(Wallet.class));
    }

    @Test
    void createTransfer_whenWalletsMissing_throwsNotFound() {
        CreateTransferRequest request = request("key-1", "wallet-a", "wallet-b", "100.00");
        when(walletRepository.existsById("wallet-a")).thenReturn(false);
        when(walletRepository.existsById("wallet-b")).thenReturn(true);

        assertThatThrownBy(() -> transferService.createTransfer(request))
                .isInstanceOf(TransferServiceException.class)
                .satisfies(ex -> {
                    TransferServiceException serviceException = (TransferServiceException) ex;
                    assertThat(serviceException.getErrorCode()).isEqualTo("WALLETS_NOT_FOUND");
                    assertThat(serviceException.getHttpStatus()).isEqualTo(404);
                });

        verify(transferRepository, never()).save(any());
        verify(idempotencyRecordRepository, never()).save(any());
    }

    @Test
    void createTransfer_whenInsufficientBalance_marksFailedAndThrows() {
        CreateTransferRequest request = request("key-1", "wallet-a", "wallet-b", "100.00");
        Wallet from = wallet("wallet-a", "40.00");
        Wallet to = wallet("wallet-b", "50.00");

        when(walletRepository.existsById("wallet-a")).thenReturn(true);
        when(walletRepository.existsById("wallet-b")).thenReturn(true);
        when(idempotencyRecordRepository.findByIdempotencyKey("key-1")).thenReturn(Optional.empty());
        stubPersistenceForFailure(from, to);

        assertThatThrownBy(() -> transferService.createTransfer(request))
                .isInstanceOf(TransferServiceException.class)
                .satisfies(ex -> {
                    TransferServiceException serviceException = (TransferServiceException) ex;
                    assertThat(serviceException.getErrorCode()).isEqualTo("INSUFFICIENT_BALANCE");
                    assertThat(serviceException.getHttpStatus()).isEqualTo(400);
                });

        verify(transferStatusService).markFailed(any(UUID.class), org.mockito.ArgumentMatchers.eq("Insufficient balance"));
        verify(ledgerEntryRepository, never()).save(any());
        verify(walletRepository, never()).save(any());
    }

    @Test
    void createTransfer_whenIdempotencyKeyExistsWithSameHashAndSuccess_returnsExisting() {
        CreateTransferRequest request = request("key-1", "wallet-a", "wallet-b", "100.00");
        String hash = RequestHash.from("wallet-a", "wallet-b", new BigDecimal("100.00"));
        Transfer existing = processedTransfer("key-1", "wallet-a", "wallet-b", "100.00");
        IdempotencyRecord record = IdempotencyRecord.create("key-1", existing.getId(), hash);

        when(walletRepository.existsById("wallet-a")).thenReturn(true);
        when(walletRepository.existsById("wallet-b")).thenReturn(true);
        when(idempotencyRecordRepository.findByIdempotencyKey("key-1")).thenReturn(Optional.of(record));
        when(transferRepository.findById(existing.getId())).thenReturn(Optional.of(existing));

        TransferResponse response = transferService.createTransfer(request);

        assertThat(response.getTransferId()).isEqualTo(existing.getId());
        assertThat(response.getState()).isEqualTo(TransferState.PROCESSED);
        verify(ledgerEntryRepository, never()).save(any());
        verify(walletRepository, never()).findByIdForUpdate(any());
    }

    @Test
    void createTransfer_whenIdempotencyKeyExistsWithDifferentHash_throwsConflict() {
        CreateTransferRequest request = request("key-1", "wallet-a", "wallet-b", "100.00");
        Transfer existing = processedTransfer("key-1", "wallet-a", "wallet-b", "50.00");
        IdempotencyRecord record = IdempotencyRecord.create(
                "key-1",
                existing.getId(),
                RequestHash.from("wallet-a", "wallet-b", new BigDecimal("50.00"))
        );

        when(walletRepository.existsById("wallet-a")).thenReturn(true);
        when(walletRepository.existsById("wallet-b")).thenReturn(true);
        when(idempotencyRecordRepository.findByIdempotencyKey("key-1")).thenReturn(Optional.of(record));

        assertThatThrownBy(() -> transferService.createTransfer(request))
                .isInstanceOf(TransferServiceException.class)
                .satisfies(ex -> {
                    TransferServiceException serviceException = (TransferServiceException) ex;
                    assertThat(serviceException.getErrorCode()).isEqualTo("IDEMPOTENCY_CONFLICT");
                    assertThat(serviceException.getHttpStatus()).isEqualTo(409);
                });

        verify(transferRepository, never()).findById(any());
    }

    @Test
    void createTransfer_whenUniqueKeyRace_recoversViaReplay() {
        CreateTransferRequest request = request("key-1", "wallet-a", "wallet-b", "100.00");
        String hash = RequestHash.from("wallet-a", "wallet-b", new BigDecimal("100.00"));
        Transfer existing = processedTransfer("key-1", "wallet-a", "wallet-b", "100.00");
        IdempotencyRecord record = IdempotencyRecord.create("key-1", existing.getId(), hash);

        when(walletRepository.existsById("wallet-a")).thenReturn(true);
        when(walletRepository.existsById("wallet-b")).thenReturn(true);
        when(idempotencyRecordRepository.findByIdempotencyKey("key-1"))
                .thenReturn(Optional.empty())
                .thenReturn(Optional.of(record));
        org.mockito.Mockito.doThrow(new org.springframework.dao.DataIntegrityViolationException(
                        "unique constraint transfers_idempotency_key_uk"
                ))
                .when(transactionTemplate)
                .execute(any());
        when(transferRepository.findById(existing.getId())).thenReturn(Optional.of(existing));

        TransferResponse response = transferService.createTransfer(request);

        assertThat(response.getTransferId()).isEqualTo(existing.getId());
        assertThat(response.getState()).isEqualTo(TransferState.PROCESSED);
        verify(transferMetrics).markUniqueKeyRace();
        verify(ledgerEntryRepository, never()).save(any());
    }

    @Test
    void createTransfer_whenNonIdempotencyIntegrityViolation_rethrows() {
        CreateTransferRequest request = request("key-1", "wallet-a", "wallet-b", "100.00");

        when(walletRepository.existsById("wallet-a")).thenReturn(true);
        when(walletRepository.existsById("wallet-b")).thenReturn(true);
        when(idempotencyRecordRepository.findByIdempotencyKey("key-1")).thenReturn(Optional.empty());
        org.springframework.dao.DataIntegrityViolationException integrityError =
                new org.springframework.dao.DataIntegrityViolationException(
                        "could not execute statement",
                        new RuntimeException("Check constraint violation: transfers_amount_ck")
                );
        org.mockito.Mockito.doThrow(integrityError).when(transactionTemplate).execute(any());

        assertThatThrownBy(() -> transferService.createTransfer(request))
                .isSameAs(integrityError);

        verify(transferMetrics, never()).markUniqueKeyRace();
        verify(idempotencyRecordRepository, org.mockito.Mockito.times(1))
                .findByIdempotencyKey("key-1");
        verify(transferRepository, never()).findById(any());
    }

    @Test
    void createTransfer_whenIdempotencyKeyExistsAndFailed_retriesAndSucceeds() {
        CreateTransferRequest request = request("key-1", "wallet-a", "wallet-b", "100.00");
        String hash = RequestHash.from("wallet-a", "wallet-b", new BigDecimal("100.00"));
        Transfer failed = Transfer.createPending("key-1", "wallet-a", "wallet-b", new BigDecimal("100.00"));
        failed.markFailed("Insufficient balance");
        IdempotencyRecord record = IdempotencyRecord.create("key-1", failed.getId(), hash);

        Wallet from = wallet("wallet-a", "500.00");
        Wallet to = wallet("wallet-b", "50.00");

        when(walletRepository.existsById("wallet-a")).thenReturn(true);
        when(walletRepository.existsById("wallet-b")).thenReturn(true);
        when(idempotencyRecordRepository.findByIdempotencyKey("key-1")).thenReturn(Optional.of(record));
        when(transferRepository.findById(failed.getId())).thenReturn(Optional.of(failed));
        storedTransfer.set(failed);
        when(transferRepository.findByIdForUpdate(failed.getId())).thenAnswer(inv -> Optional.of(storedTransfer.get()));
        when(transferRepository.save(any(Transfer.class))).thenAnswer(inv -> {
            Transfer t = inv.getArgument(0);
            storedTransfer.set(t);
            return t;
        });
        when(walletRepository.findByIdForUpdate("wallet-a")).thenReturn(Optional.of(from));
        when(walletRepository.findByIdForUpdate("wallet-b")).thenReturn(Optional.of(to));
        when(walletRepository.save(any(Wallet.class))).thenAnswer(inv -> inv.getArgument(0));
        when(ledgerEntryRepository.save(any(LedgerEntry.class))).thenAnswer(inv -> inv.getArgument(0));

        TransferResponse response = transferService.createTransfer(request);

        assertThat(response.getState()).isEqualTo(TransferState.PROCESSED);
        assertThat(response.getTransferId()).isEqualTo(failed.getId());
        assertThat(from.getBalance()).isEqualByComparingTo("400.00");
        assertThat(to.getBalance()).isEqualByComparingTo("150.00");

        ArgumentCaptor<Transfer> transferCaptor = ArgumentCaptor.forClass(Transfer.class);
        verify(transferRepository, atLeastOnce()).save(transferCaptor.capture());
        assertThat(transferCaptor.getAllValues().stream().anyMatch(t -> t.getState() == TransferState.RETRY
                || t.getState() == TransferState.PROCESSED)).isTrue();
    }

    private void stubPersistence(Wallet from, Wallet to) {
        stubTransferAndWalletLocks(from, to);
        when(walletRepository.save(any(Wallet.class))).thenAnswer(inv -> inv.getArgument(0));
        when(ledgerEntryRepository.save(any(LedgerEntry.class))).thenAnswer(inv -> inv.getArgument(0));
    }

    private void stubPersistenceForFailure(Wallet from, Wallet to) {
        stubTransferAndWalletLocks(from, to);
    }

    private void stubTransferAndWalletLocks(Wallet from, Wallet to) {
        when(transferRepository.save(any(Transfer.class))).thenAnswer(inv -> {
            Transfer t = inv.getArgument(0);
            storedTransfer.set(t);
            return t;
        });
        when(idempotencyRecordRepository.save(any(IdempotencyRecord.class)))
                .thenAnswer(inv -> inv.getArgument(0));
        when(transferRepository.findByIdForUpdate(any(UUID.class)))
                .thenAnswer(inv -> Optional.ofNullable(storedTransfer.get()));
        when(walletRepository.findByIdForUpdate("wallet-a")).thenReturn(Optional.of(from));
        when(walletRepository.findByIdForUpdate("wallet-b")).thenReturn(Optional.of(to));
    }

    private static CreateTransferRequest request(
            String key,
            String from,
            String to,
            String amount
    ) {
        return CreateTransferRequest.builder()
                .idempotencyKey(key)
                .fromWalletId(from)
                .toWalletId(to)
                .amount(new BigDecimal(amount))
                .build();
    }

    private static Wallet wallet(String id, String balance) {
        Instant now = Instant.parse("2026-09-13T09:00:00Z");
        return Wallet.builder()
                .id(id)
                .userId(1L)
                .balance(new BigDecimal(balance))
                .createdAt(now)
                .updatedAt(now)
                .build();
    }

    private static Transfer processedTransfer(String key, String from, String to, String amount) {
        Transfer transfer = Transfer.createPending(key, from, to, new BigDecimal(amount));
        transfer.markProcessed();
        return transfer;
    }
}
