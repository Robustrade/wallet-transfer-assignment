package com.wallet.transfer.domain;

import org.junit.jupiter.api.Test;

import java.math.BigDecimal;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Unit Tests for Transfer Domain Entity
 * 
 * Tests transfer state machine:
 * - Valid state transitions
 * - Invalid state transitions
 * - Business rule validation
 */
class TransferTest {
    
    @Test
    void testTransferCreation() {
        Transfer transfer = Transfer.builder()
                .transferId("txn-001")
                .fromWalletId("wallet-001")
                .toWalletId("wallet-002")
                .amount(new BigDecimal("50.00"))
                .idempotencyKey("req-123")
                .status(TransferStatus.PENDING)
                .build();
        
        assertNotNull(transfer);
        assertEquals("txn-001", transfer.getTransferId());
        assertEquals(TransferStatus.PENDING, transfer.getStatus());
    }
    
    @Test
    void testTransferValidation_ValidTransfer() {
        Transfer transfer = Transfer.builder()
                .transferId("txn-001")
                .fromWalletId("wallet-001")
                .toWalletId("wallet-002")
                .amount(new BigDecimal("50.00"))
                .idempotencyKey("req-123")
                .status(TransferStatus.PENDING)
                .build();
        
        // Should not throw
        assertDoesNotThrow(transfer::validate);
    }
    
    @Test
    void testTransferValidation_SameWalletIds() {
        Transfer transfer = Transfer.builder()
                .transferId("txn-001")
                .fromWalletId("wallet-001")
                .toWalletId("wallet-001")
                .amount(new BigDecimal("50.00"))
                .idempotencyKey("req-123")
                .status(TransferStatus.PENDING)
                .build();
        
        assertThrows(IllegalArgumentException.class, transfer::validate);
    }
    
    @Test
    void testTransferValidation_NegativeAmount() {
        Transfer transfer = Transfer.builder()
                .transferId("txn-001")
                .fromWalletId("wallet-001")
                .toWalletId("wallet-002")
                .amount(new BigDecimal("-50.00"))
                .idempotencyKey("req-123")
                .status(TransferStatus.PENDING)
                .build();
        
        assertThrows(IllegalArgumentException.class, transfer::validate);
    }
    
    @Test
    void testTransferValidation_NullFromWalletId() {
        Transfer transfer = Transfer.builder()
                .transferId("txn-001")
                .fromWalletId(null)
                .toWalletId("wallet-002")
                .amount(new BigDecimal("50.00"))
                .idempotencyKey("req-123")
                .status(TransferStatus.PENDING)
                .build();
        
        assertThrows(Exception.class, transfer::validate);
    }
    
    @Test
    void testMarkProcessed() {
        Transfer transfer = Transfer.builder()
                .transferId("txn-001")
                .fromWalletId("wallet-001")
                .toWalletId("wallet-002")
                .amount(new BigDecimal("50.00"))
                .idempotencyKey("req-123")
                .status(TransferStatus.PENDING)
                .build();
        
        transfer.markProcessed();
        
        assertEquals(TransferStatus.PROCESSED, transfer.getStatus());
    }
    
    @Test
    void testMarkFailed() {
        Transfer transfer = Transfer.builder()
                .transferId("txn-001")
                .fromWalletId("wallet-001")
                .toWalletId("wallet-002")
                .amount(new BigDecimal("50.00"))
                .idempotencyKey("req-123")
                .status(TransferStatus.PENDING)
                .build();
        
        transfer.markFailed();
        
        assertEquals(TransferStatus.FAILED, transfer.getStatus());
    }
    
    @Test
    void testInvalidStateTransition_MarkProcessedTwice() {
        Transfer transfer = Transfer.builder()
                .transferId("txn-001")
                .fromWalletId("wallet-001")
                .toWalletId("wallet-002")
                .amount(new BigDecimal("50.00"))
                .idempotencyKey("req-123")
                .status(TransferStatus.PENDING)
                .build();
        
        transfer.markProcessed();
        
        // Second markProcessed should fail
        assertThrows(IllegalStateException.class, transfer::markProcessed);
    }
}
