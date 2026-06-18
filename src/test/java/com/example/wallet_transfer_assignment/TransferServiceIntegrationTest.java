package com.example.wallet_transfer_assignment;

import com.example.wallet_transfer_assignment.dto.TransferRequest;
import com.example.wallet_transfer_assignment.dto.TransferResponse;
import com.example.wallet_transfer_assignment.entity.Transfer;
import com.example.wallet_transfer_assignment.entity.TransferState;
import com.example.wallet_transfer_assignment.entity.Wallet;
import com.example.wallet_transfer_assignment.exception.InsufficientBalanceException;
import com.example.wallet_transfer_assignment.exception.InvalidTransferException;
import com.example.wallet_transfer_assignment.repository.IdempotencyRecordRepository;
import com.example.wallet_transfer_assignment.repository.LedgerEntryRepository;
import com.example.wallet_transfer_assignment.repository.TransferRepository;
import com.example.wallet_transfer_assignment.repository.WalletRepository;
import com.example.wallet_transfer_assignment.service.TransferService;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.data.redis.core.RedisTemplate;
import org.springframework.data.redis.core.ValueOperations;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.context.bean.override.mockito.MockitoBean;

import java.math.BigDecimal;
import java.util.UUID;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.when;

@SpringBootTest
@ActiveProfiles("test")
class TransferServiceIntegrationTest {

    @Autowired
    private TransferService transferService;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @Autowired
    private IdempotencyRecordRepository idempotencyRecordRepository;

    @MockitoBean
    private RedisTemplate<String, String> redisTemplate;

    @MockitoBean
    private ValueOperations<String, String> valueOperations;

    private Wallet senderWallet;
    private Wallet receiverWallet;

    @BeforeEach
    void setup() {

        when(redisTemplate.opsForValue())
                .thenReturn(valueOperations);

        when(valueOperations.get(anyString()))
                .thenReturn(null);

        senderWallet = walletRepository.save(
                Wallet.builder()
                        .ownerName("Alice-" + UUID.randomUUID())
                        .balance(new BigDecimal("1000"))
                        .build()
        );

        receiverWallet = walletRepository.save(
                Wallet.builder()
                        .ownerName("Bob-" + UUID.randomUUID())
                        .balance(new BigDecimal("500"))
                        .build()
        );
    }

    @Test
    void shouldTransferMoneySuccessfully() {

        TransferRequest request =
                new TransferRequest(
                        "txn-1",
                        senderWallet.getId(),
                        receiverWallet.getId(),
                        new BigDecimal("100")
                );

        TransferResponse response =
                transferService.transfer(request);

        Wallet updatedSender =
                walletRepository.findById(senderWallet.getId())
                        .orElseThrow();

        Wallet updatedReceiver =
                walletRepository.findById(receiverWallet.getId())
                        .orElseThrow();

        assertNotNull(response);
        assertNotNull(response.transferId());

        assertEquals(
                0,
                updatedSender.getBalance()
                        .compareTo(new BigDecimal("900"))
        );

        assertEquals(
                0,
                updatedReceiver.getBalance()
                        .compareTo(new BigDecimal("600"))
        );
    }

    @Test
    void shouldThrowExceptionWhenBalanceInsufficient() {

        TransferRequest request =
                new TransferRequest(
                        "txn-2",
                        senderWallet.getId(),
                        receiverWallet.getId(),
                        new BigDecimal("5000")
                );

        assertThrows(
                InsufficientBalanceException.class,
                () -> transferService.transfer(request)
        );
    }

    @Test
    void shouldRejectSameWalletTransfer() {

        TransferRequest request =
                new TransferRequest(
                        "txn-3",
                        senderWallet.getId(),
                        senderWallet.getId(),
                        new BigDecimal("100")
                );

        assertThrows(
                InvalidTransferException.class,
                () -> transferService.transfer(request)
        );
    }

    @Test
    void shouldCreateTwoLedgerEntriesForTransfer() {

        long ledgerCountBefore =
                ledgerEntryRepository.count();

        TransferRequest request =
                new TransferRequest(
                        "txn-4",
                        senderWallet.getId(),
                        receiverWallet.getId(),
                        new BigDecimal("100")
                );

        transferService.transfer(request);

        assertEquals(
                ledgerCountBefore + 2,
                ledgerEntryRepository.count()
        );
    }

    @Test
    void shouldReturnSameResponseForDuplicateIdempotencyKey() {

        long transferCountBefore =
                transferRepository.count();

        long ledgerCountBefore =
                ledgerEntryRepository.count();

        long idempotencyCountBefore =
                idempotencyRecordRepository.count();

        TransferRequest request =
                new TransferRequest(
                        "txn-idempotent-1",
                        senderWallet.getId(),
                        receiverWallet.getId(),
                        new BigDecimal("100")
                );

        TransferResponse firstResponse =
                transferService.transfer(request);

        TransferResponse secondResponse =
                transferService.transfer(request);

        assertEquals(
                firstResponse.transferId(),
                secondResponse.transferId()
        );

        assertEquals(
                transferCountBefore + 1,
                transferRepository.count()
        );

        assertEquals(
                ledgerCountBefore + 2,
                ledgerEntryRepository.count()
        );

        assertEquals(
                idempotencyCountBefore + 1,
                idempotencyRecordRepository.count()
        );
    }

    @Test
    void shouldReadTransferFromRedisCache() {

        String transferId =
                UUID.randomUUID().toString();

        when(valueOperations.get(
                "idempotency:txn-cache"))
                .thenReturn(transferId);

        Transfer transfer = Transfer.builder()
                .id(UUID.fromString(transferId))
                .fromWallet(senderWallet)
                .toWallet(receiverWallet)
                .amount(new BigDecimal("100"))
                .state(TransferState.PROCESSED)
                .build();

        transferRepository.save(transfer);

        TransferResponse response =
                transferService.transfer(
                        new TransferRequest(
                                "txn-cache",
                                senderWallet.getId(),
                                receiverWallet.getId(),
                                new BigDecimal("100")
                        )
                );

        assertEquals(
                transfer.getId(),
                response.transferId()
        );
    }

    @Test
    void shouldPreventDoubleSpendingUnderConcurrency()
            throws Exception {

        ExecutorService executor =
                Executors.newFixedThreadPool(2);

        Runnable task1 = () -> {
            try {
                transferService.transfer(
                        new TransferRequest(
                                UUID.randomUUID().toString(),
                                senderWallet.getId(),
                                receiverWallet.getId(),
                                new BigDecimal("1000")
                        )
                );
            } catch (Exception ignored) {
            }
        };

        Runnable task2 = () -> {
            try {
                transferService.transfer(
                        new TransferRequest(
                                UUID.randomUUID().toString(),
                                senderWallet.getId(),
                                receiverWallet.getId(),
                                new BigDecimal("1000")
                        )
                );
            } catch (Exception ignored) {
            }
        };

        executor.submit(task1);
        executor.submit(task2);

        executor.shutdown();

        executor.awaitTermination(
                10,
                TimeUnit.SECONDS
        );

        Wallet updatedSender =
                walletRepository.findById(senderWallet.getId())
                        .orElseThrow();

        Wallet updatedReceiver =
                walletRepository.findById(receiverWallet.getId())
                        .orElseThrow();

        assertEquals(
                0,
                updatedSender.getBalance()
                        .compareTo(BigDecimal.ZERO)
        );

        assertEquals(
                0,
                updatedReceiver.getBalance()
                        .compareTo(new BigDecimal("1500"))
        );
    }


}
