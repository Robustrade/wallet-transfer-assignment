package com.robustrade.wallet_transfer_service;

import org.junit.jupiter.api.Test;

class TransferServiceTest {

    @Test
    void shouldTransferMoneySuccessfully() {
        // wallet1 = 1000
        // wallet2 = 500

        // transfer 100

        // verify:
        // wallet1 = 900
        // wallet2 = 600
    }

    @Test
    void shouldReturnSameTransferForSameIdempotencyKey() {

        // call transfer with key "abc123"

        // call again with same key

        // verify same transfer id returned
    }

    @Test
    void shouldThrowExceptionWhenBalanceIsInsufficient() {

        // wallet balance = 100

        // try transfer = 1000

        // expect InsufficientBalanceException
    }
}