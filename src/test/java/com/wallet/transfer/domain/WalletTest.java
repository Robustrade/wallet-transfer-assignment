package com.wallet.transfer.domain;

import org.junit.jupiter.api.Test;

import java.math.BigDecimal;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Unit Tests for Wallet Domain Entity
 * 
 * Tests core wallet behavior:
 * - Debit operations
 * - Credit operations
 * - Balance validation
 * - Constraint enforcement
 */
class WalletTest {
    
    @Test
    void testWalletCreation() {
        Wallet wallet = Wallet.builder()
                .walletId("wallet-001")
                .balance(new BigDecimal("100.00"))
                .build();
        
        assertNotNull(wallet);
        assertEquals("wallet-001", wallet.getWalletId());
        assertEquals(new BigDecimal("100.00"), wallet.getBalance());
    }
    
    @Test
    void testCreditOperation() {
        Wallet wallet = Wallet.builder()
                .walletId("wallet-001")
                .balance(new BigDecimal("100.00"))
                .build();
        
        wallet.credit(new BigDecimal("50.00"));
        
        assertEquals(new BigDecimal("150.00"), wallet.getBalance());
    }
    
    @Test
    void testDebitOperation() {
        Wallet wallet = Wallet.builder()
                .walletId("wallet-001")
                .balance(new BigDecimal("100.00"))
                .build();
        
        wallet.debit(new BigDecimal("30.00"));
        
        assertEquals(new BigDecimal("70.00"), wallet.getBalance());
    }
    
    @Test
    void testDebitExceedingBalance() {
        Wallet wallet = Wallet.builder()
                .walletId("wallet-001")
                .balance(new BigDecimal("50.00"))
                .build();
        
        assertThrows(IllegalArgumentException.class, () -> {
            wallet.debit(new BigDecimal("100.00"));
        });
    }
    
    @Test
    void testDebitNegativeAmount() {
        Wallet wallet = Wallet.builder()
                .walletId("wallet-001")
                .balance(new BigDecimal("100.00"))
                .build();
        
        assertThrows(IllegalArgumentException.class, () -> {
            wallet.debit(new BigDecimal("-50.00"));
        });
    }
    
    @Test
    void testCreditNegativeAmount() {
        Wallet wallet = Wallet.builder()
                .walletId("wallet-001")
                .balance(new BigDecimal("100.00"))
                .build();
        
        assertThrows(IllegalArgumentException.class, () -> {
            wallet.credit(new BigDecimal("-50.00"));
        });
    }
    
    @Test
    void testBalanceNeverNegative() {
        Wallet wallet = Wallet.builder()
                .walletId("wallet-001")
                .balance(new BigDecimal("100.00"))
                .build();
        
        wallet.debit(new BigDecimal("100.00"));
        
        assertTrue(wallet.getBalance().compareTo(BigDecimal.ZERO) >= 0);
    }
}
