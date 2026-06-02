package com.walletservice.application.service;

import static org.assertj.core.api.Assertions.*;

import com.walletservice.application.exception.IdempotencyConflictException;
import com.walletservice.application.util.HashUtil;
import com.walletservice.domain.entity.*;
import com.walletservice.infrastructure.repository.*;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import java.util.*;

import static org.mockito.Mockito.*;

/**
 * Unit tests for TransferService.
 *
 * These tests validate the core business logic:
 * - Idempotency behavior
 * - Transfer creation and state transitions
 * - Ledger entry creation
 * - Wallet balance updates
 * - Error handling
 */
@ExtendWith(MockitoExtension.class)
@DisplayName("TransferService")
class TransferServiceTest {
    private TransferService transferService;

    @Mock
    private TransferRepository transferRepository;

    @Mock
    private WalletRepository walletRepository;

    @Mock
    private LedgerEntryRepository ledgerEntryRepository;

    @Mock
    private IdempotencyRecordRepository idempotencyRecordRepository;

    private String sourceWalletId;
    private String destinationWalletId;
    private Long transferAmount;
    private String idempotencyKey;

    @BeforeEach
    void setUp() {
        transferService = new TransferService(
            transferRepository,
            walletRepository,
            ledgerEntryRepository,
            idempotencyRecordRepository
        );

        sourceWalletId = UUID.randomUUID().toString();
        destinationWalletId = UUID.randomUUID().toString();
        transferAmount = 100L;
        idempotencyKey = "test-key-123";
    }

    @Test
    @DisplayName("Should successfully create transfer when wallets exist and balance is sufficient")
    void testTransferCreation_Success() {
        // Arrange
        Wallet sourceWallet = Wallet.builder()
            .id(sourceWalletId)
            .balance(1000L)
            .version(0L)
            .build();

        Wallet destinationWallet = Wallet.builder()
            .id(destinationWalletId)
            .balance(500L)
            .version(0L)
            .build();

        Transfer savedTransfer = Transfer.builder()
            .id(UUID.randomUUID().toString())
            .fromWalletId(sourceWalletId)
            .toWalletId(destinationWalletId)
            .amount(transferAmount)
            .status(Transfer.TransferStatus.PENDING)
            .build();

        when(idempotencyRecordRepository.findById(idempotencyKey))
            .thenReturn(Optional.empty());
        when(walletRepository.findByIdForUpdate(sourceWalletId))
            .thenReturn(Optional.of(sourceWallet));
        when(walletRepository.findByIdForUpdate(destinationWalletId))
            .thenReturn(Optional.of(destinationWallet));
        when(transferRepository.save(any(Transfer.class)))
            .thenReturn(savedTransfer);

        // Act
        Transfer result = transferService.createTransfer(
            sourceWalletId,
            destinationWalletId,
            transferAmount,
            idempotencyKey
        );

        // Assert
        assertThat(result).isNotNull();
        assertThat(result.getStatus()).isEqualTo(Transfer.TransferStatus.PROCESSED);
        assertThat(result.getAmount()).isEqualTo(transferAmount);

        // Verify repository interactions
        verify(transferRepository, times(2)).save(any(Transfer.class));
        verify(walletRepository, times(2)).save(any(Wallet.class));
        verify(ledgerEntryRepository, times(2)).save(any(LedgerEntry.class));
        verify(idempotencyRecordRepository).save(any(IdempotencyRecord.class));
    }

    @Test
    @DisplayName("Should return original transfer on duplicate idempotent request")
    void testTransferCreation_Idempotency_DuplicateRequest() {
        // Arrange
        String transferId = UUID.randomUUID().toString();
        Transfer originalTransfer = Transfer.builder()
            .id(transferId)
            .fromWalletId(sourceWalletId)
            .toWalletId(destinationWalletId)
            .amount(transferAmount)
            .status(Transfer.TransferStatus.PROCESSED)
            .build();

        IdempotencyRecord idempotencyRecord = IdempotencyRecord.builder()
            .idempotencyKey(idempotencyKey)
            .transferId(transferId)
            .requestHash(HashUtil.hashTransferRequest(sourceWalletId, destinationWalletId, transferAmount))
            .build();

        when(idempotencyRecordRepository.findById(idempotencyKey))
            .thenReturn(Optional.of(idempotencyRecord));
        when(transferRepository.findById(transferId))
            .thenReturn(Optional.of(originalTransfer));

        // Act
        Transfer result = transferService.createTransfer(
            sourceWalletId,
            destinationWalletId,
            transferAmount,
            idempotencyKey
        );

        // Assert
        assertThat(result).isNotNull();
        assertThat(result.getId()).isEqualTo(transferId);
        assertThat(result.getStatus()).isEqualTo(Transfer.TransferStatus.PROCESSED);

        // Verify no new wallets were locked or ledger entries created
        verify(walletRepository, never()).findByIdForUpdate(any());
        verify(ledgerEntryRepository, never()).save(any());
    }

    @Test
    @DisplayName("Should reject idempotency key reuse with different parameters")
    void testTransferCreation_Idempotency_ConflictingRequest() {
        // Arrange
        String differentDestinationWalletId = UUID.randomUUID().toString();
        String differentRequestHash = "different-hash";

        IdempotencyRecord existingRecord = IdempotencyRecord.builder()
            .idempotencyKey(idempotencyKey)
            .transferId(UUID.randomUUID().toString())
            .requestHash(differentRequestHash)
            .build();

        when(idempotencyRecordRepository.findById(idempotencyKey))
            .thenReturn(Optional.of(existingRecord));

        // Act & Assert
        assertThatThrownBy(() ->
            transferService.createTransfer(
                sourceWalletId,
                differentDestinationWalletId,
                transferAmount,
                idempotencyKey
            )
        )
            .isInstanceOf(IdempotencyConflictException.class)
            .hasMessageContaining("already used with different transfer parameters");
    }

    @Test
    @DisplayName("Should throw exception when source wallet not found")
    void testTransferCreation_SourceWalletNotFound() {
        // Arrange
        when(idempotencyRecordRepository.findById(idempotencyKey))
            .thenReturn(Optional.empty());
        when(walletRepository.findByIdForUpdate(sourceWalletId))
            .thenReturn(Optional.empty());

        // Act & Assert
        assertThatThrownBy(() ->
            transferService.createTransfer(
                sourceWalletId,
                destinationWalletId,
                transferAmount,
                idempotencyKey
            )
        )
            .isInstanceOf(IllegalArgumentException.class)
            .hasMessageContaining("Source wallet not found");
    }

    @Test
    @DisplayName("Should throw exception when destination wallet not found")
    void testTransferCreation_DestinationWalletNotFound() {
        // Arrange
        Wallet sourceWallet = Wallet.builder()
            .id(sourceWalletId)
            .balance(1000L)
            .version(0L)
            .build();

        when(idempotencyRecordRepository.findById(idempotencyKey))
            .thenReturn(Optional.empty());
        when(walletRepository.findByIdForUpdate(sourceWalletId))
            .thenReturn(Optional.of(sourceWallet));
        when(walletRepository.findByIdForUpdate(destinationWalletId))
            .thenReturn(Optional.empty());

        // Act & Assert
        assertThatThrownBy(() ->
            transferService.createTransfer(
                sourceWalletId,
                destinationWalletId,
                transferAmount,
                idempotencyKey
            )
        )
            .isInstanceOf(IllegalArgumentException.class)
            .hasMessageContaining("Destination wallet not found");
    }

    @Test
    @DisplayName("Should throw exception when source wallet has insufficient balance")
    void testTransferCreation_InsufficientBalance() {
        // Arrange
        Wallet sourceWallet = Wallet.builder()
            .id(sourceWalletId)
            .balance(50L)  // Less than transfer amount
            .version(0L)
            .build();

        Wallet destinationWallet = Wallet.builder()
            .id(destinationWalletId)
            .balance(500L)
            .version(0L)
            .build();

        when(idempotencyRecordRepository.findById(idempotencyKey))
            .thenReturn(Optional.empty());
        when(walletRepository.findByIdForUpdate(sourceWalletId))
            .thenReturn(Optional.of(sourceWallet));
        when(walletRepository.findByIdForUpdate(destinationWalletId))
            .thenReturn(Optional.of(destinationWallet));

        // Act & Assert
        assertThatThrownBy(() ->
            transferService.createTransfer(
                sourceWalletId,
                destinationWalletId,
                transferAmount,
                idempotencyKey
            )
        )
            .isInstanceOf(IllegalStateException.class)
            .hasMessageContaining("Insufficient balance");
    }

    @Test
    @DisplayName("Should throw exception when source and destination are the same")
    void testTransferCreation_SameSourceAndDestination() {
        // Arrange
        Wallet wallet = Wallet.builder()
            .id(sourceWalletId)
            .balance(1000L)
            .version(0L)
            .build();

        when(idempotencyRecordRepository.findById(idempotencyKey))
            .thenReturn(Optional.empty());
        when(walletRepository.findByIdForUpdate(sourceWalletId))
            .thenReturn(Optional.of(wallet));
        when(walletRepository.findByIdForUpdate(sourceWalletId))
            .thenReturn(Optional.of(wallet));

        // Act & Assert
        assertThatThrownBy(() ->
            transferService.createTransfer(
                sourceWalletId,
                sourceWalletId,
                transferAmount,
                idempotencyKey
            )
        )
            .isInstanceOf(IllegalArgumentException.class)
            .hasMessageContaining("Source and destination wallets cannot be the same");
    }

    @Test
    @DisplayName("Should throw exception when transfer amount is invalid")
    void testTransferCreation_InvalidAmount() {
        // Arrange
        Wallet sourceWallet = Wallet.builder()
            .id(sourceWalletId)
            .balance(1000L)
            .version(0L)
            .build();

        Wallet destinationWallet = Wallet.builder()
            .id(destinationWalletId)
            .balance(500L)
            .version(0L)
            .build();

        when(idempotencyRecordRepository.findById(idempotencyKey))
            .thenReturn(Optional.empty());
        when(walletRepository.findByIdForUpdate(sourceWalletId))
            .thenReturn(Optional.of(sourceWallet));
        when(walletRepository.findByIdForUpdate(destinationWalletId))
            .thenReturn(Optional.of(destinationWallet));

        // Act & Assert
        assertThatThrownBy(() ->
            transferService.createTransfer(
                sourceWalletId,
                destinationWalletId,
                -100L,
                idempotencyKey
            )
        )
            .isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    @DisplayName("Should successfully retrieve transfer by ID")
    void testGetTransfer_Success() {
        // Arrange
        UUID transferId = UUID.randomUUID();
        Transfer transfer = Transfer.builder()
            .id(transferId.toString())
            .fromWalletId(sourceWalletId)
            .toWalletId(destinationWalletId)
            .amount(transferAmount)
            .status(Transfer.TransferStatus.PROCESSED)
            .build();

        when(transferRepository.findById(transferId.toString()))
            .thenReturn(Optional.of(transfer));

        // Act
        Transfer result = transferService.getTransfer(transferId.toString());

        // Assert
        assertThat(result).isNotNull();
        assertThat(result.getId()).isEqualTo(transferId.toString());
    }

    @Test
    @DisplayName("Should throw exception when transfer not found")
    void testGetTransfer_NotFound() {
        // Arrange
        UUID transferId = UUID.randomUUID();
        when(transferRepository.findById(transferId.toString()))
            .thenReturn(Optional.empty());

        // Act & Assert
        assertThatThrownBy(() -> transferService.getTransfer(transferId.toString()))
            .isInstanceOf(IllegalArgumentException.class)
            .hasMessageContaining("Transfer not found");
    }
}

