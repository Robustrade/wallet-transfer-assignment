package com.wallet.transfer;

import com.wallet.transfer.domain.LedgerEntry;
import com.wallet.transfer.domain.LedgerEntryType;
import com.wallet.transfer.domain.Transfer;
import com.wallet.transfer.domain.TransferState;
import com.wallet.transfer.domain.Wallet;
import com.wallet.transfer.dto.CreateTransferRequest;
import com.wallet.transfer.dto.TransferResponse;
import com.wallet.transfer.exception.IdempotencyConflictException;
import com.wallet.transfer.exception.InsufficientFundsException;
import com.wallet.transfer.exception.WalletNotFoundException;
import com.wallet.transfer.repository.IdempotencyRecordRepository;
import com.wallet.transfer.repository.LedgerEntryRepository;
import com.wallet.transfer.repository.TransferRepository;
import com.wallet.transfer.repository.WalletRepository;
import com.wallet.transfer.service.TransferService;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ActiveProfiles;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.Callable;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import static org.assertj.core.api.Assertions.assertThat;
import static org.junit.jupiter.api.Assertions.assertThrows;

@SpringBootTest
@ActiveProfiles("test")
public class WalletTransferIntegrationTests {

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

    @BeforeEach
    public void setup() {
        ledgerEntryRepository.deleteAll();
        transferRepository.deleteAll();
        idempotencyRecordRepository.deleteAll();
        walletRepository.deleteAll();

        walletRepository.save(new Wallet("wallet_1", 1000L));
        walletRepository.save(new Wallet("wallet_2", 500L));
        walletRepository.save(new Wallet("wallet_3", 0L));
    }

    @Test
    public void testSuccessfulTransfer() {
        CreateTransferRequest request = new CreateTransferRequest(
                UUID.randomUUID().toString(),
                "wallet_1",
                "wallet_2",
                200L
        );

        TransferResponse response = transferService.createTransfer(request);

        assertThat(response.transferId()).startsWith("T-");
        assertThat(response.fromWalletId()).isEqualTo("wallet_1");
        assertThat(response.toWalletId()).isEqualTo("wallet_2");
        assertThat(response.amount()).isEqualTo(200L);
        assertThat(response.state()).isEqualTo(TransferState.PROCESSED);

        Wallet fromWallet = walletRepository.findById("wallet_1").orElseThrow();
        Wallet toWallet = walletRepository.findById("wallet_2").orElseThrow();
        assertThat(fromWallet.getBalance()).isEqualTo(800L);
        assertThat(toWallet.getBalance()).isEqualTo(700L);

        List<LedgerEntry> entries = ledgerEntryRepository.findAll();
        assertThat(entries).hasSize(2);

        LedgerEntry debit = entries.stream().filter(e -> e.getType() == LedgerEntryType.DEBIT).findFirst().orElseThrow();
        LedgerEntry credit = entries.stream().filter(e -> e.getType() == LedgerEntryType.CREDIT).findFirst().orElseThrow();

        assertThat(debit.getWalletId()).isEqualTo("wallet_1");
        assertThat(debit.getAmount()).isEqualTo(200L);
        assertThat(debit.getTransferId()).isEqualTo(response.transferId());

        assertThat(credit.getWalletId()).isEqualTo("wallet_2");
        assertThat(credit.getAmount()).isEqualTo(200L);
        assertThat(credit.getTransferId()).isEqualTo(response.transferId());
    }

    @Test
    public void testInsufficientFunds() {
        CreateTransferRequest request = new CreateTransferRequest(
                UUID.randomUUID().toString(),
                "wallet_1",
                "wallet_2",
                2000L
        );

        assertThrows(InsufficientFundsException.class, () -> transferService.createTransfer(request));

        Wallet fromWallet = walletRepository.findById("wallet_1").orElseThrow();
        Wallet toWallet = walletRepository.findById("wallet_2").orElseThrow();
        assertThat(fromWallet.getBalance()).isEqualTo(1000L);
        assertThat(toWallet.getBalance()).isEqualTo(500L);

        List<Transfer> transfers = transferRepository.findAll();
        assertThat(transfers).hasSize(1);
        assertThat(transfers.get(0).getState()).isEqualTo(TransferState.FAILED);

        assertThat(ledgerEntryRepository.count()).isEqualTo(0);
    }

    @Test
    public void testWalletNotFound() {
        CreateTransferRequest request = new CreateTransferRequest(
                UUID.randomUUID().toString(),
                "wallet_nonexistent",
                "wallet_2",
                100L
        );

        assertThrows(WalletNotFoundException.class, () -> transferService.createTransfer(request));
    }

    @Test
    public void testIdempotencyDeduplication() {
        String key = "idempotency-key-test";
        CreateTransferRequest request1 = new CreateTransferRequest(
                key,
                "wallet_1",
                "wallet_2",
                100L
        );

        TransferResponse response1 = transferService.createTransfer(request1);
        assertThat(response1.state()).isEqualTo(TransferState.PROCESSED);

        assertThat(walletRepository.findById("wallet_1").orElseThrow().getBalance()).isEqualTo(900L);

        TransferResponse response2 = transferService.createTransfer(request1);

        assertThat(response2.transferId()).isEqualTo(response1.transferId());
        assertThat(response2.amount()).isEqualTo(response1.amount());
        assertThat(response2.state()).isEqualTo(response1.state());

        assertThat(walletRepository.findById("wallet_1").orElseThrow().getBalance()).isEqualTo(900L);
        assertThat(ledgerEntryRepository.count()).isEqualTo(2);
    }

    @Test
    public void testIdempotencyConflictWithDifferentParameters() {
        String key = "idempotency-key-conflict-test";
        CreateTransferRequest request1 = new CreateTransferRequest(
                key,
                "wallet_1",
                "wallet_2",
                100L
        );

        transferService.createTransfer(request1);

        CreateTransferRequest request2 = new CreateTransferRequest(
                key,
                "wallet_1",
                "wallet_2",
                200L
        );

        assertThrows(IdempotencyConflictException.class, () -> transferService.createTransfer(request2));
    }

    @Test
    public void testSelfTransferRejected() {
        CreateTransferRequest request = new CreateTransferRequest(
                UUID.randomUUID().toString(),
                "wallet_1",
                "wallet_1",
                100L
        );

        assertThrows(IllegalArgumentException.class, () -> transferService.createTransfer(request));

        assertThat(transferRepository.count()).isEqualTo(0);
        assertThat(ledgerEntryRepository.count()).isEqualTo(0);
        assertThat(walletRepository.findById("wallet_1").orElseThrow().getBalance()).isEqualTo(1000L);
    }

    @Test
    public void testLedgerAlwaysBalances() {
        transferService.createTransfer(new CreateTransferRequest(UUID.randomUUID().toString(), "wallet_1", "wallet_2", 300L));
        transferService.createTransfer(new CreateTransferRequest(UUID.randomUUID().toString(), "wallet_2", "wallet_1", 100L));
        transferService.createTransfer(new CreateTransferRequest(UUID.randomUUID().toString(), "wallet_1", "wallet_3", 50L));

        List<LedgerEntry> entries = ledgerEntryRepository.findAll();
        long totalDebits = entries.stream().filter(e -> e.getType() == LedgerEntryType.DEBIT).mapToLong(LedgerEntry::getAmount).sum();
        long totalCredits = entries.stream().filter(e -> e.getType() == LedgerEntryType.CREDIT).mapToLong(LedgerEntry::getAmount).sum();

        assertThat(totalDebits).isEqualTo(totalCredits);
        assertThat(entries).hasSize(6);
    }

    @Test
    public void testConcurrentTransfersCorrectBalance() throws Exception {
        int totalRequests = 12;
        ExecutorService executor = Executors.newFixedThreadPool(totalRequests);
        List<Callable<TransferResponse>> tasks = new ArrayList<>();

        for (int i = 0; i < totalRequests; i++) {
            final String idKey = "concurrent-key-" + i;
            tasks.add(() -> transferService.createTransfer(new CreateTransferRequest(
                    idKey,
                    "wallet_1",
                    "wallet_2",
                    100L
            )));
        }

        List<Future<TransferResponse>> futures = executor.invokeAll(tasks);
        executor.shutdown();

        int successCount = 0;
        int failCount = 0;

        for (Future<TransferResponse> future : futures) {
            try {
                TransferResponse res = future.get();
                if (res != null && res.state() == TransferState.PROCESSED) {
                    successCount++;
                }
            } catch (Exception ex) {
                Throwable cause = ex.getCause();
                if (cause instanceof InsufficientFundsException) {
                    failCount++;
                } else {
                    throw ex;
                }
            }
        }

        assertThat(successCount).isEqualTo(10);
        assertThat(failCount).isEqualTo(2);

        Wallet wallet1 = walletRepository.findById("wallet_1").orElseThrow();
        Wallet wallet2 = walletRepository.findById("wallet_2").orElseThrow();
        assertThat(wallet1.getBalance()).isEqualTo(0L);
        assertThat(wallet2.getBalance()).isEqualTo(1500L);

        long debitEntriesSum = ledgerEntryRepository.findAll().stream()
                .filter(e -> e.getType() == LedgerEntryType.DEBIT)
                .mapToLong(LedgerEntry::getAmount)
                .sum();

        long creditEntriesSum = ledgerEntryRepository.findAll().stream()
                .filter(e -> e.getType() == LedgerEntryType.CREDIT)
                .mapToLong(LedgerEntry::getAmount)
                .sum();

        assertThat(debitEntriesSum).isEqualTo(1000L);
        assertThat(creditEntriesSum).isEqualTo(1000L);
        assertThat(ledgerEntryRepository.count()).isEqualTo(20L);
    }
}
