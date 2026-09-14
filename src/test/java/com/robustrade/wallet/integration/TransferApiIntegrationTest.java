package com.robustrade.wallet.integration;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.robustrade.wallet.domain.TransferState;
import com.robustrade.wallet.handler.dto.TransferResponse;
import com.robustrade.wallet.handler.dto.WalletResponse;
import java.math.BigDecimal;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.springframework.test.web.servlet.MvcResult;

class TransferApiIntegrationTest extends AbstractIntegrationTest {

    @Test
    void createTransfer_movesFundsWritesLedgerAndMarksSuccess() throws Exception {
        WalletResponse from = createWallet(2001L, "500.00");
        WalletResponse to = createWallet(2002L, "50.00");

        TransferResponse response = createTransfer("xfer-success-1", from.getWalletId(), to.getWalletId(), "100.00");

        assertThat(response.getState()).isEqualTo(TransferState.PROCESSED);
        assertThat(response.getTransferId()).isNotNull();
        assertThat(response.getFailureReason()).isNull();
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("400.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("150.00");
        assertThat(transferState(response.getTransferId())).isEqualTo("PROCESSED");
        assertThat(ledgerEntryCount(response.getTransferId())).isEqualTo(2);
    }

    @Test
    void createTransfer_whenWalletMissing_returnsNotFound() throws Exception {
        WalletResponse from = createWallet(2003L, "100.00");

        MvcResult result = performTransfer(
                        "xfer-missing-wallet",
                        from.getWalletId(),
                        UUID.randomUUID().toString(),
                        "10.00"
                )
                .andExpect(status().isNotFound())
                .andReturn();

        assertThat(errorCode(result)).isEqualTo("WALLETS_NOT_FOUND");
    }

    @Test
    void createTransfer_whenInsufficientBalance_marksFailedAndLeavesBalancesUnchanged() throws Exception {
        WalletResponse from = createWallet(2004L, "40.00");
        WalletResponse to = createWallet(2005L, "10.00");

        MvcResult result = performTransfer("xfer-insufficient", from.getWalletId(), to.getWalletId(), "100.00")
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("INSUFFICIENT_BALANCE"))
                .andReturn();

        assertThat(errorCode(result)).isEqualTo("INSUFFICIENT_BALANCE");
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("40.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("10.00");

        UUID transferId = jdbcTemplate.queryForObject(
                "SELECT transfer_id FROM idempotency_records WHERE idempotency_key = ?",
                UUID.class,
                "xfer-insufficient"
        );
        assertThat(transferState(transferId)).isEqualTo("FAILED");
        assertThat(ledgerEntryCount(transferId)).isEqualTo(0);
    }

    @Test
    void createTransfer_whenSameIdempotencyKeyAndPayload_returnsSameTransferWithoutDoubleDebit()
            throws Exception {
        WalletResponse from = createWallet(2006L, "500.00");
        WalletResponse to = createWallet(2007L, "0.00");

        TransferResponse first = createTransfer("xfer-idempotent", from.getWalletId(), to.getWalletId(), "75.00");
        TransferResponse second = createTransfer("xfer-idempotent", from.getWalletId(), to.getWalletId(), "75.00");

        assertThat(second.getTransferId()).isEqualTo(first.getTransferId());
        assertThat(second.getState()).isEqualTo(TransferState.PROCESSED);
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("425.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("75.00");
        assertThat(ledgerEntryCount(first.getTransferId())).isEqualTo(2);
    }

    @Test
    void createTransfer_whenSameKeyAndEquivalentAmountScale_replaysWithoutConflict() throws Exception {
        WalletResponse from = createWallet(2020L, "500.00");
        WalletResponse to = createWallet(2021L, "0.00");

        TransferResponse first = createTransfer("xfer-scale-hash", from.getWalletId(), to.getWalletId(), "100.00");
        TransferResponse second = createTransfer("xfer-scale-hash", from.getWalletId(), to.getWalletId(), "100.0");

        assertThat(second.getTransferId()).isEqualTo(first.getTransferId());
        assertThat(second.getState()).isEqualTo(TransferState.PROCESSED);
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("400.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("100.00");
        assertThat(ledgerEntryCount(first.getTransferId())).isEqualTo(2);
    }

    @Test
    void createTransfer_whenSameIdempotencyKeyDifferentPayload_returnsConflict() throws Exception {
        WalletResponse from = createWallet(2008L, "500.00");
        WalletResponse to = createWallet(2009L, "0.00");

        createTransfer("xfer-conflict", from.getWalletId(), to.getWalletId(), "50.00");

        MvcResult result = performTransfer("xfer-conflict", from.getWalletId(), to.getWalletId(), "60.00")
                .andExpect(status().isConflict())
                .andExpect(jsonPath("$.errorCode").value("IDEMPOTENCY_CONFLICT"))
                .andReturn();

        assertThat(errorCode(result)).isEqualTo("IDEMPOTENCY_CONFLICT");
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("450.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("50.00");
    }

    @Test
    void createTransfer_whenPreviouslyFailed_retriesAndSucceedsAfterTopUp() throws Exception {
        WalletResponse from = createWallet(2010L, "40.00");
        WalletResponse to = createWallet(2011L, "0.00");

        performTransfer("xfer-retry", from.getWalletId(), to.getWalletId(), "100.00")
                .andExpect(status().isBadRequest());

        jdbcTemplate.update(
                "UPDATE wallets SET balance = ? WHERE id = ?",
                new BigDecimal("250.00"),
                from.getWalletId()
        );

        TransferResponse retried = createTransfer("xfer-retry", from.getWalletId(), to.getWalletId(), "100.00");

        assertThat(retried.getState()).isEqualTo(TransferState.PROCESSED);
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("150.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("100.00");
        assertThat(transferState(retried.getTransferId())).isEqualTo("PROCESSED");
        assertThat(ledgerEntryCount(retried.getTransferId())).isEqualTo(2);
    }

    @Test
    void createTransfer_whenAmountInvalid_returnsValidationError() throws Exception {
        WalletResponse from = createWallet(2012L, "100.00");
        WalletResponse to = createWallet(2013L, "0.00");

        performTransfer("xfer-invalid-amount", from.getWalletId(), to.getWalletId(), "0.00")
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"));
    }

    @Test
    void createTransfer_whenAmountHasTooManyFractionDigits_returnsValidationError() throws Exception {
        WalletResponse from = createWallet(2017L, "100.00");
        WalletResponse to = createWallet(2018L, "0.00");

        performTransfer("xfer-scale-invalid", from.getWalletId(), to.getWalletId(), "1.001")
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"))
                .andExpect(jsonPath("$.message")
                        .value("amount must match NUMERIC(15,2) (max 13 digits before decimal, 2 after)"));
    }

    @Test
    void createTransfer_whenIdempotencyKeyTooLong_returnsValidationError() throws Exception {
        WalletResponse from = createWallet(2015L, "100.00");
        WalletResponse to = createWallet(2016L, "0.00");
        String tooLongKey = "k".repeat(129);

        performTransfer(tooLongKey, from.getWalletId(), to.getWalletId(), "10.00")
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"))
                .andExpect(jsonPath("$.message")
                        .value("idempotencyKey must be at most 128 characters"));
    }

    @Test
    void createTransfer_whenWalletsAreSame_returnsValidationError() throws Exception {
        WalletResponse wallet = createWallet(2014L, "100.00");

        performTransfer("xfer-same-wallet", wallet.getWalletId(), wallet.getWalletId(), "10.00")
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errorCode").value("VALIDATION_ERROR"));
    }
}
