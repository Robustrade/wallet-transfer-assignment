package com.walletTransfer.walletTransfer.service;


import com.walletTransfer.walletTransfer.dto.TransferRequest;
import com.walletTransfer.walletTransfer.dto.TransferResponse;
import com.walletTransfer.walletTransfer.entity.*;
import com.walletTransfer.walletTransfer.enums.TransferStatus;
import com.walletTransfer.walletTransfer.exception.WalletNotFoundException;
import com.walletTransfer.walletTransfer.mapper.TransferMapper;
import com.walletTransfer.walletTransfer.repository.LedgerEntryRepository;
import com.walletTransfer.walletTransfer.repository.TransferRepository;
import com.walletTransfer.walletTransfer.repository.WalletRepository;
import com.walletTransfer.walletTransfer.service.HashingService;
import com.walletTransfer.walletTransfer.service.IdempotencyService;
import com.walletTransfer.walletTransfer.service.TransferDomainService;
import com.walletTransfer.walletTransfer.service.impl.TransferServiceImpl;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import java.util.List;
import java.util.Optional;
import java.util.UUID;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class TransferServiceImplTest {

    @Mock
    private WalletRepository walletRepository;

    @Mock
    private TransferRepository transferRepository;

    @Mock
    private LedgerEntryRepository ledgerEntryRepository;

    @Mock
    private TransferDomainService transferDomainService;

    @Mock
    private IdempotencyService idempotencyService;

    @Mock
    private HashingService hashingService;

    @Mock
    private TransferMapper transferMapper;

    @InjectMocks
    private TransferServiceImpl transferService;

    @Test
    void shouldReturnExistingResponseWhenIdempotencyKeyAlreadyExists() {

        TransferRequest request = TransferRequest.builder()
                .idempotencyKey("transfer-001")
                .build();

        TransferResponse response = TransferResponse.builder().build();

        when(hashingService.generateHash(request))
                .thenReturn("hash");

        when(idempotencyService.findExistingResponse(
                "transfer-001",
                "hash"))
                .thenReturn(Optional.of(response));

        TransferResponse result =
                transferService.createTransfer(request);

        assertEquals(response, result);
    }

    @Test
    void shouldThrowExceptionWhenSourceWalletNotFound() {

        UUID sourceId = UUID.randomUUID();
        UUID destinationId = UUID.randomUUID();

        TransferRequest request = TransferRequest.builder()
                .idempotencyKey("key")
                .fromWalletId(sourceId)
                .toWalletId(destinationId)
                .amount(100L)
                .build();

        when(hashingService.generateHash(any()))
                .thenReturn("hash");

        when(idempotencyService.findExistingResponse(any(), any()))
                .thenReturn(Optional.empty());

        when(idempotencyService.createPendingRecord(any(), any()))
                .thenReturn(mock(IdempotencyRecord.class));

        when(walletRepository.findAndLockWallets(any()))
                .thenReturn(List.of());

        assertThrows(
                WalletNotFoundException.class,
                () -> transferService.createTransfer(request)
        );
    }

    @Test
    void shouldCreateTransferSuccessfully() {

        UUID sourceId = UUID.randomUUID();
        UUID destinationId = UUID.randomUUID();

        Wallet sourceWallet = Wallet.builder()
                .id(sourceId)
                .balance(1000L)
                .build();

        Wallet destinationWallet = Wallet.builder()
                .id(destinationId)
                .balance(500L)
                .build();

        TransferRequest request = TransferRequest.builder()
                .idempotencyKey("key")
                .fromWalletId(sourceId)
                .toWalletId(destinationId)
                .amount(100L)
                .build();

        IdempotencyRecord record =
                mock(IdempotencyRecord.class);

        TransferResponse response =
                TransferResponse.builder()
                        .status(TransferStatus.PROCESSED)
                        .build();

        when(hashingService.generateHash(any()))
                .thenReturn("hash");

        when(idempotencyService.findExistingResponse(any(), any()))
                .thenReturn(Optional.empty());

        when(idempotencyService.createPendingRecord(any(), any()))
                .thenReturn(record);

        when(walletRepository.findAndLockWallets(any()))
                .thenReturn(List.of(
                        sourceWallet,
                        destinationWallet));

        when(transferDomainService.processTransfer(
                any(),
                any(),
                any()))
                .thenReturn(List.of());

        when(transferMapper.toResponse(any()))
                .thenReturn(response);

        TransferResponse result =
                transferService.createTransfer(request);

        assertNotNull(result);

        verify(walletRepository)
                .save(sourceWallet);

        verify(walletRepository)
                .save(destinationWallet);

        verify(idempotencyService)
                .markCompleted(
                        eq(record),
                        any(),
                        eq(response));
    }
}
