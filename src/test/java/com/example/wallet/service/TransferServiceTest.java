package com.example.wallet.service;
import com.example.wallet.exception.InsufficientFundsException;
import com.example.wallet.exception.WalletNotFoundException;
import com.example.wallet.handler.dto.CreateTransferRequest;
import com.example.wallet.handler.dto.TransferResponse;
import com.example.wallet.model.IdempotencyRecord;
import com.example.wallet.model.Transfer;
import com.example.wallet.model.TransferStatus;
import com.example.wallet.model.Wallet;
import com.example.wallet.repository.IdempotencyRepository;
import com.example.wallet.repository.LedgerRepository;
import com.example.wallet.repository.TransferRepository;
import com.example.wallet.repository.WalletRepository;
import com.example.wallet.service.impl.TransferServiceImpl;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import io.micrometer.core.instrument.simple.SimpleMeterRegistry;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.test.util.ReflectionTestUtils;
import org.springframework.transaction.TransactionStatus;
import org.springframework.transaction.support.TransactionCallback;
import org.springframework.transaction.support.TransactionTemplate;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyList;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;
@ExtendWith(MockitoExtension.class)
class TransferServiceTest {
    @Mock private TransferRepository      transferRepository;
    @Mock private WalletRepository        walletRepository;
    @Mock private LedgerRepository        ledgerRepository;
    @Mock private IdempotencyRepository   idempotencyRepository;
    @Mock private TransactionTemplate     transactionTemplate;
    private TransferServiceImpl transferService;
    private ObjectMapper        objectMapper;
    private static final String FROM_ID  = "wallet_alice";
    private static final String TO_ID    = "wallet_bob";
    private static final String IDEM_KEY = "test-key-001";
    private static final BigDecimal AMOUNT = new BigDecimal("100.00");
    @BeforeEach
    void setUp() {
        objectMapper = new ObjectMapper().registerModule(new JavaTimeModule());
        TransferMetrics metrics = new TransferMetrics(new SimpleMeterRegistry());
        transferService = new TransferServiceImpl(
                transferRepository, walletRepository, ledgerRepository,
                idempotencyRepository, transactionTemplate, objectMapper, metrics);
        ReflectionTestUtils.setField(transferService, "idempotencyTtlHours", 24);
        org.mockito.Mockito.lenient()
                .when(transactionTemplate.execute(any(TransactionCallback.class)))
                .thenAnswer(inv ->
                        inv.getArgument(0, TransactionCallback.class)
                           .doInTransaction(mock(TransactionStatus.class)));
    }
    @Test
    @DisplayName("successful transfer: PROCESSED, wallets debited/credited, ledger written")
    void execute_success_processesTransferAndWritesLedger() {
        when(idempotencyRepository.find(IDEM_KEY, TransferServiceImpl.ENDPOINT)).thenReturn(Optional.empty());
        Transfer pending = pendingTransfer(IDEM_KEY);
        when(transferRepository.insertPending(any())).thenReturn(pending);
        when(walletRepository.lockForUpdate(anyList()))
                .thenReturn(List.of(wallet(FROM_ID, new BigDecimal("500")), wallet(TO_ID, new BigDecimal("200"))));
        TransferResponse response = transferService.execute(request(IDEM_KEY, FROM_ID, TO_ID, AMOUNT));
        assertThat(response.status()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(response.id()).isEqualTo(pending.id());
        assertThat(response.newlyCreated()).isTrue();
        verify(walletRepository).debit(FROM_ID, AMOUNT);
        verify(walletRepository).credit(TO_ID, AMOUNT);
        verify(ledgerRepository).insertDebit(pending.id(), FROM_ID, AMOUNT);
        verify(ledgerRepository).insertCredit(pending.id(), TO_ID, AMOUNT);
        verify(transferRepository).updateStatus(pending.id(), TransferStatus.PROCESSED);
        verify(idempotencyRepository).save(eq(IDEM_KEY), eq(TransferServiceImpl.ENDPOINT), anyString(), eq(201), any());
    }
    @Test
    @DisplayName("insufficient funds: throws InsufficientFundsException, no ledger entries, FAILED committed")
    void execute_insufficientFunds_throwsAndWritesNoLedger() {
        when(idempotencyRepository.find(IDEM_KEY, TransferServiceImpl.ENDPOINT)).thenReturn(Optional.empty());
        Transfer pending = pendingTransfer(IDEM_KEY);
        when(transferRepository.insertPending(any())).thenReturn(pending);
        when(walletRepository.lockForUpdate(anyList()))
                .thenReturn(List.of(wallet(FROM_ID, new BigDecimal("50")), wallet(TO_ID, BigDecimal.ZERO)));
        assertThatThrownBy(() -> transferService.execute(request(IDEM_KEY, FROM_ID, TO_ID, AMOUNT)))
                .isInstanceOf(InsufficientFundsException.class);
        verify(transferRepository).updateStatus(pending.id(), TransferStatus.FAILED);
        verify(walletRepository, never()).debit(anyString(), any());
        verify(walletRepository, never()).credit(anyString(), any());
        verify(ledgerRepository, never()).insertDebit(any(), anyString(), any());
        verify(ledgerRepository, never()).insertCredit(any(), anyString(), any());
    }
    @Test
    @DisplayName("idempotent replay (fast-path): returns cached response without opening transaction")
    void execute_idempotentReplay_fastPath_returnsCachedWithoutTransaction() throws Exception {
        TransferResponse cached = processedResponse(UUID.randomUUID(), IDEM_KEY);
        String json = objectMapper.writeValueAsString(cached);
        when(idempotencyRepository.find(IDEM_KEY, TransferServiceImpl.ENDPOINT))
                .thenReturn(Optional.of(new IdempotencyRecord(IDEM_KEY, TransferServiceImpl.ENDPOINT, json, 201,
                        Instant.now(), Instant.now().plusSeconds(86400))));
        TransferResponse response = transferService.execute(request(IDEM_KEY, FROM_ID, TO_ID, AMOUNT));
        assertThat(response.id()).isEqualTo(cached.id());
        assertThat(response.status()).isEqualTo(TransferStatus.PROCESSED);
        verify(transactionTemplate, never()).execute(any());
        verify(transferRepository, never()).insertPending(any());
    }
    @Test
    @DisplayName("idempotent replay of FAILED: re-throws InsufficientFundsException without opening transaction")
    void execute_idempotentReplay_failed_throwsWithoutTransaction() throws Exception {
        TransferResponse failedCached = new TransferResponse(UUID.randomUUID(), IDEM_KEY,
                FROM_ID, TO_ID, AMOUNT, TransferStatus.FAILED, Instant.now(), false);
        String json = objectMapper.writeValueAsString(failedCached);
        when(idempotencyRepository.find(IDEM_KEY, TransferServiceImpl.ENDPOINT))
                .thenReturn(Optional.of(new IdempotencyRecord(IDEM_KEY, TransferServiceImpl.ENDPOINT, json, 422,
                        Instant.now(), Instant.now().plusSeconds(86400))));
        assertThatThrownBy(() -> transferService.execute(request(IDEM_KEY, FROM_ID, TO_ID, AMOUNT)))
                .isInstanceOf(InsufficientFundsException.class);
        verify(transactionTemplate, never()).execute(any());
    }
    @Test
    @DisplayName("duplicate key: ledger insert called exactly once across two calls")
    void execute_duplicateKey_ledgerWrittenOnlyOnce() throws Exception {
        Transfer pending = pendingTransfer(IDEM_KEY);
        when(transferRepository.insertPending(any())).thenReturn(pending);
        when(walletRepository.lockForUpdate(anyList()))
                .thenReturn(List.of(wallet(FROM_ID, new BigDecimal("500")), wallet(TO_ID, BigDecimal.ZERO)));
        when(idempotencyRepository.find(IDEM_KEY, TransferServiceImpl.ENDPOINT)).thenReturn(Optional.empty());
        transferService.execute(request(IDEM_KEY, FROM_ID, TO_ID, AMOUNT));
        String json = objectMapper.writeValueAsString(processedResponse(pending.id(), IDEM_KEY));
        when(idempotencyRepository.find(IDEM_KEY, TransferServiceImpl.ENDPOINT))
                .thenReturn(Optional.of(new IdempotencyRecord(IDEM_KEY, TransferServiceImpl.ENDPOINT, json, 201,
                        Instant.now(), Instant.now().plusSeconds(86400))));
        transferService.execute(request(IDEM_KEY, FROM_ID, TO_ID, AMOUNT));
        verify(ledgerRepository).insertDebit(pending.id(), FROM_ID, AMOUNT);
        verify(ledgerRepository).insertCredit(pending.id(), TO_ID, AMOUNT);
    }
    @Test
    @DisplayName("source wallet not found: throws WalletNotFoundException")
    void execute_sourceWalletNotFound_throws() {
        when(idempotencyRepository.find(anyString(), anyString())).thenReturn(Optional.empty());
        when(walletRepository.lockForUpdate(anyList())).thenReturn(List.of(wallet(TO_ID, BigDecimal.TEN)));
        assertThatThrownBy(() -> transferService.execute(request(IDEM_KEY, FROM_ID, TO_ID, AMOUNT)))
                .isInstanceOf(WalletNotFoundException.class)
                .hasMessageContaining(FROM_ID);
    }
    @Test
    @DisplayName("destination wallet not found: throws WalletNotFoundException")
    void execute_destinationWalletNotFound_throws() {
        when(idempotencyRepository.find(anyString(), anyString())).thenReturn(Optional.empty());
        when(walletRepository.lockForUpdate(anyList())).thenReturn(List.of(wallet(FROM_ID, new BigDecimal("500"))));
        assertThatThrownBy(() -> transferService.execute(request(IDEM_KEY, FROM_ID, TO_ID, AMOUNT)))
                .isInstanceOf(WalletNotFoundException.class)
                .hasMessageContaining(TO_ID);
    }
    private CreateTransferRequest request(String key, String from, String to, BigDecimal amount) {
        return new CreateTransferRequest(key, from, to, amount);
    }
    private Transfer pendingTransfer(String key) {
        return new Transfer(UUID.randomUUID(), key, FROM_ID, TO_ID, AMOUNT, TransferStatus.PENDING, Instant.now(), Instant.now());
    }
    private Wallet wallet(String id, BigDecimal balance) { return new Wallet(id, balance, Instant.now()); }
    private TransferResponse processedResponse(UUID id, String key) {
        return new TransferResponse(id, key, FROM_ID, TO_ID, AMOUNT, TransferStatus.PROCESSED, Instant.now(), false);
    }
}