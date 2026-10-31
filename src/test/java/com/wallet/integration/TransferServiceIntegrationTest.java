package com.wallet.integration;

import com.wallet.domain.LedgerEntryType;
import com.wallet.domain.Wallet;
import com.wallet.repository.LedgerEntryRepository;
import com.wallet.repository.WalletRepository;
import com.wallet.service.TransferService;
import com.wallet.dto.CreateTransferRequest;
import com.wallet.dto.TransferResponse;
import com.wallet.exception.IdempotencyKeyReusedException;
import com.wallet.exception.WalletNotFoundException;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;

import java.math.BigDecimal;
import java.util.UUID;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
class TransferServiceIntegrationTest {

    @Autowired
    private TransferService transferService;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    private String newWallet(BigDecimal balance) {
        String id = "w_" + UUID.randomUUID();
        walletRepository.save(new Wallet(id, balance));
        return id;
    }

    @Test
    void transfersMoneyAndUpdatesBothBalances() {
        String from = newWallet(new BigDecimal("100.00"));
        String to = newWallet(new BigDecimal("10.00"));

        TransferResponse response = transferService.createTransfer(
                new CreateTransferRequest(UUID.randomUUID().toString(), from, to, new BigDecimal("30.00")));

        assertThat(response.status()).isEqualTo("PROCESSED");
        assertThat(response.replayed()).isFalse();

        assertThat(walletRepository.findById(from).orElseThrow().getBalance())
                .isEqualByComparingTo("70.00");
        assertThat(walletRepository.findById(to).orElseThrow().getBalance())
                .isEqualByComparingTo("40.00");
    }

    @Test
    void producesExactlyTwoBalancedLedgerEntries() {
        String from = newWallet(new BigDecimal("100.00"));
        String to = newWallet(new BigDecimal("0.00"));

        TransferResponse response = transferService.createTransfer(
                new CreateTransferRequest(UUID.randomUUID().toString(), from, to, new BigDecimal("25.00")));

        var entries = ledgerEntryRepository.findByTransferId(response.transferId());
        assertThat(entries).hasSize(2);

        var debit = entries.stream().filter(e -> e.getEntryType() == LedgerEntryType.DEBIT).findFirst().orElseThrow();
        var credit = entries.stream().filter(e -> e.getEntryType() == LedgerEntryType.CREDIT).findFirst().orElseThrow();

        assertThat(debit.getWalletId()).isEqualTo(from);
        assertThat(credit.getWalletId()).isEqualTo(to);
        assertThat(debit.getAmount()).isEqualByComparingTo(credit.getAmount());
        assertThat(debit.getAmount()).isEqualByComparingTo("25.00");
    }

    @Test
    void duplicateRequestWithSameIdempotencyKeyReturnsOriginalResultWithoutDoubleMoving() {
        String from = newWallet(new BigDecimal("100.00"));
        String to = newWallet(new BigDecimal("0.00"));
        String key = UUID.randomUUID().toString();
        CreateTransferRequest request = new CreateTransferRequest(key, from, to, new BigDecimal("40.00"));

        TransferResponse first = transferService.createTransfer(request);
        TransferResponse second = transferService.createTransfer(request);

        assertThat(second.transferId()).isEqualTo(first.transferId());
        assertThat(second.replayed()).isTrue();

        // Balance must reflect exactly ONE transfer, not two.
        assertThat(walletRepository.findById(from).orElseThrow().getBalance()).isEqualByComparingTo("60.00");
        assertThat(walletRepository.findById(to).orElseThrow().getBalance()).isEqualByComparingTo("40.00");
        assertThat(ledgerEntryRepository.findByTransferId(first.transferId())).hasSize(2);
    }

    @Test
    void reusingIdempotencyKeyWithDifferentPayloadIsRejected() {
        String from = newWallet(new BigDecimal("100.00"));
        String to = newWallet(new BigDecimal("0.00"));
        String key = UUID.randomUUID().toString();

        transferService.createTransfer(new CreateTransferRequest(key, from, to, new BigDecimal("10.00")));

        assertThatThrownBy(() ->
                transferService.createTransfer(new CreateTransferRequest(key, from, to, new BigDecimal("20.00"))))
                .isInstanceOf(IdempotencyKeyReusedException.class);
    }

    @Test
    void insufficientBalanceMarksTransferFailedWithoutMovingMoney() {
        String from = newWallet(new BigDecimal("10.00"));
        String to = newWallet(new BigDecimal("0.00"));

        TransferResponse response = transferService.createTransfer(
                new CreateTransferRequest(UUID.randomUUID().toString(), from, to, new BigDecimal("500.00")));

        assertThat(response.status()).isEqualTo("FAILED");
        assertThat(response.failureReason()).isNotBlank();

        assertThat(walletRepository.findById(from).orElseThrow().getBalance()).isEqualByComparingTo("10.00");
        assertThat(walletRepository.findById(to).orElseThrow().getBalance()).isEqualByComparingTo("0.00");
        assertThat(ledgerEntryRepository.findByTransferId(response.transferId())).isEmpty();
    }

    @Test
    void retryAfterInsufficientBalanceFailureReplaysTheSameFailure() {
        String from = newWallet(new BigDecimal("10.00"));
        String to = newWallet(new BigDecimal("0.00"));
        String key = UUID.randomUUID().toString();
        CreateTransferRequest request = new CreateTransferRequest(key, from, to, new BigDecimal("500.00"));

        TransferResponse first = transferService.createTransfer(request);
        TransferResponse second = transferService.createTransfer(request);

        assertThat(first.status()).isEqualTo("FAILED");
        assertThat(second.transferId()).isEqualTo(first.transferId());
        assertThat(second.replayed()).isTrue();
        assertThat(second.status()).isEqualTo("FAILED");
    }

    @Test
    void transferToOrFromUnknownWalletIsRejectedAndDoesNotClaimTheIdempotencyKey() {
        String from = newWallet(new BigDecimal("100.00"));
        String bogusTo = "does-not-exist-" + UUID.randomUUID();
        String key = UUID.randomUUID().toString();

        assertThatThrownBy(() ->
                transferService.createTransfer(new CreateTransferRequest(key, from, bogusTo, new BigDecimal("10.00"))))
                .isInstanceOf(WalletNotFoundException.class);

        // The key was never claimed, so retrying with a valid wallet under
        // the same key must be allowed to proceed normally.
        String realTo = newWallet(new BigDecimal("0.00"));
        TransferResponse retried = transferService.createTransfer(
                new CreateTransferRequest(key, from, realTo, new BigDecimal("10.00")));
        assertThat(retried.status()).isEqualTo("PROCESSED");
        assertThat(retried.replayed()).isFalse();
    }

    @Test
    void rejectsSelfTransfer() {
        String wallet = newWallet(new BigDecimal("100.00"));
        assertThatThrownBy(() ->
                transferService.createTransfer(
                        new CreateTransferRequest(UUID.randomUUID().toString(), wallet, wallet, new BigDecimal("10.00"))))
                .isInstanceOf(IllegalArgumentException.class);
    }
}
