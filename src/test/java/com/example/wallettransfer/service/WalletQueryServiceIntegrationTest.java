package com.example.wallettransfer.service;

import com.example.wallettransfer.dto.CreateTransferRequest;
import com.example.wallettransfer.dto.TransferHistoryResponse;
import com.example.wallettransfer.exception.WalletNotFoundException;
import com.example.wallettransfer.testcontainer.PostgresTestContainer;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;

import java.math.BigDecimal;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

class WalletQueryServiceIntegrationTest extends PostgresTestContainer {

    @Autowired private WalletQueryService walletQueryService;
    @Autowired private TransferService transferService;

    @Test
    void returnsTheCurrentWalletBalance() {
        createWallet("wallet", "42.5000");

        var wallet = walletQueryService.getWallet("wallet");

        assertThat(wallet.walletId()).isEqualTo("wallet");
        assertThat(wallet.balance()).isEqualByComparingTo("42.5000");
    }

    @Test
    void returnsTransfersForBothDirections() {
        createWallet("wallet", "100.0000");
        createWallet("other", "0.0000");
        transferService.createTransfer(new CreateTransferRequest("out", "wallet", "other", new BigDecimal("10")));
        transferService.createTransfer(new CreateTransferRequest("in", "other", "wallet", new BigDecimal("5")));

        TransferHistoryResponse history = walletQueryService.getTransferHistory("wallet", 50, null);

        assertThat(history.transfers()).hasSize(2);
        assertThat(history.transfers()).allSatisfy(transfer ->
                assertThat(transfer.fromWalletId().equals("wallet") || transfer.toWalletId().equals("wallet")).isTrue());
        assertThat(history.nextCursor()).isNull();
    }

    @Test
    void rejectsAnUnknownWallet() {
        assertThatThrownBy(() -> walletQueryService.getWallet("missing"))
                .isInstanceOf(WalletNotFoundException.class);
    }
}
