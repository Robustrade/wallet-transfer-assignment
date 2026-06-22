package com.walletTransfer.walletTransfer.service;

import com.walletTransfer.walletTransfer.entity.LedgerEntry;
import com.walletTransfer.walletTransfer.entity.Transfer;
import com.walletTransfer.walletTransfer.entity.Wallet;
import com.walletTransfer.walletTransfer.exception.InsufficientBalanceException;
import com.walletTransfer.walletTransfer.factory.LedgerEntryFactory;
import com.walletTransfer.walletTransfer.service.impl.TransferDomainServiceImpl;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import java.util.List;
import java.util.UUID;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class TransferDomainServiceImplTest {

    @Mock
    private LedgerEntryFactory ledgerEntryFactory;

    @InjectMocks
    private TransferDomainServiceImpl transferDomainService;

    @Test
    void shouldProcessTransferSuccessfully() {

        Wallet sourceWallet = mock(Wallet.class);
        Wallet destinationWallet = mock(Wallet.class);
        Transfer transfer = mock(Transfer.class);

        LedgerEntry debitEntry = mock(LedgerEntry.class);
        LedgerEntry creditEntry = mock(LedgerEntry.class);

        when(transfer.getAmount()).thenReturn(100L);

        when(sourceWallet.hasSufficientBalance(100L))
                .thenReturn(true);

        when(ledgerEntryFactory.createDebitEntry(
                sourceWallet,
                transfer,
                100L))
                .thenReturn(debitEntry);

        when(ledgerEntryFactory.createCreditEntry(
                destinationWallet,
                transfer,
                100L))
                .thenReturn(creditEntry);

        List<LedgerEntry> result =
                transferDomainService.processTransfer(
                        sourceWallet,
                        destinationWallet,
                        transfer);

        assertEquals(2, result.size());

        verify(sourceWallet).debit(100L);
        verify(destinationWallet).credit(100L);
        verify(transfer).markProcessed();
    }

    @Test
    void shouldThrowExceptionWhenBalanceIsInsufficient() {

        Wallet sourceWallet = mock(Wallet.class);
        Wallet destinationWallet = mock(Wallet.class);
        Transfer transfer = mock(Transfer.class);

        when(transfer.getAmount()).thenReturn(500L);

        when(sourceWallet.hasSufficientBalance(500L))
                .thenReturn(false);

        assertThrows(
                InsufficientBalanceException.class,
                () -> transferDomainService.processTransfer(
                        sourceWallet,
                        destinationWallet,
                        transfer)
        );

        verify(sourceWallet, never())
                .debit(anyLong());

        verify(destinationWallet, never())
                .credit(anyLong());
    }

    @Test
    void shouldCreateDebitAndCreditLedgerEntries() {

        Wallet sourceWallet = mock(Wallet.class);
        Wallet destinationWallet = mock(Wallet.class);
        Transfer transfer = mock(Transfer.class);

        LedgerEntry debitEntry = mock(LedgerEntry.class);
        LedgerEntry creditEntry = mock(LedgerEntry.class);

        when(transfer.getAmount()).thenReturn(200L);

        when(sourceWallet.hasSufficientBalance(200L))
                .thenReturn(true);

        when(ledgerEntryFactory.createDebitEntry(
                any(),
                any(),
                anyLong()))
                .thenReturn(debitEntry);

        when(ledgerEntryFactory.createCreditEntry(
                any(),
                any(),
                anyLong()))
                .thenReturn(creditEntry);

        List<LedgerEntry> result =
                transferDomainService.processTransfer(
                        sourceWallet,
                        destinationWallet,
                        transfer);

        assertTrue(result.contains(debitEntry));
        assertTrue(result.contains(creditEntry));
    }
}