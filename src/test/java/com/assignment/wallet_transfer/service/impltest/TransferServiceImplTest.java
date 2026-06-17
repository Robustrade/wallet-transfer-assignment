package com.assignment.wallet_transfer.service.impltest;

import com.assignment.wallet_transfer.dtos.TransferRequest;
import com.assignment.wallet_transfer.dtos.TransferResponse;
import com.assignment.wallet_transfer.entity.LedgerEntry;
import com.assignment.wallet_transfer.entity.Transfer;
import com.assignment.wallet_transfer.entity.Wallet;
import com.assignment.wallet_transfer.enums.EntryType;
import com.assignment.wallet_transfer.enums.TransferStatus;
import com.assignment.wallet_transfer.exception.InsufficientBalanceException;
import com.assignment.wallet_transfer.repository.LedgerRepository;
import com.assignment.wallet_transfer.repository.TransferRepository;
import com.assignment.wallet_transfer.repository.WalletRepository;
import com.assignment.wallet_transfer.service.impl.TransferServiceImpl;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.springframework.test.context.junit.jupiter.SpringExtension;

import java.math.BigDecimal;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

@ExtendWith(SpringExtension.class)
public class TransferServiceImplTest {

    @InjectMocks
    private TransferServiceImpl transferService;

    @Mock
    private TransferRepository transferRepository;

    @Mock
    private WalletRepository walletRepository;

    @Mock
    private LedgerRepository ledgerRepository;

    @Test
    void shouldTransferMoneySuccessfully() {

        Wallet source = Wallet.builder()
                .id(UUID.randomUUID())
                .balance(BigDecimal.valueOf(1000))
                .build();

        Wallet destination = Wallet.builder()
                .id(UUID.randomUUID())
                .balance(BigDecimal.valueOf(500))
                .build();

        when(transferRepository.findByIdempotencyKey("abc"))
                .thenReturn(Optional.empty());

        when(transferRepository.save(any(Transfer.class)))
                .thenAnswer(invocation -> invocation.getArgument(0));

        when(walletRepository.findById(source.getId()))
                .thenReturn(Optional.of(source));

        when(walletRepository.findById(destination.getId()))
                .thenReturn(Optional.of(destination));

        TransferRequest request =
                new TransferRequest(
                        source.getId(),
                        destination.getId(),
                        BigDecimal.valueOf(100)
                );

        TransferResponse response =
                transferService.transfer(request, "abc");

        assertEquals(
                BigDecimal.valueOf(900),
                source.getBalance());

        assertEquals(
                BigDecimal.valueOf(600),
                destination.getBalance());

        assertEquals(
                TransferStatus.PROCESSED,
                response.status());
    }

    @Test
    void shouldRejectTransferWhenBalanceInsufficient() {

        Wallet source = Wallet.builder()
                .id(UUID.randomUUID())
                .balance(BigDecimal.valueOf(10))
                .build();

        Wallet destination = Wallet.builder()
                .id(UUID.randomUUID())
                .balance(BigDecimal.valueOf(500))
                .build();

        when(transferRepository.findByIdempotencyKey("abc"))
                .thenReturn(Optional.empty());

        when(walletRepository.findById(source.getId()))
                .thenReturn(Optional.of(source));

        when(walletRepository.findById(destination.getId()))
                .thenReturn(Optional.of(destination));

        TransferRequest request =
                new TransferRequest(
                        source.getId(),
                        destination.getId(),
                        BigDecimal.valueOf(100)
                );

        assertThrows(
                InsufficientBalanceException.class,
                () -> transferService.transfer(request, "abc")
        );
    }

    @Test
    void shouldReturnExistingTransferForDuplicateIdempotencyKey() {

        Transfer existing =
                Transfer.builder()
                        .id(UUID.randomUUID())
                        .amount(BigDecimal.valueOf(100))
                        .status(TransferStatus.PROCESSED)
                        .build();

        when(transferRepository.findByIdempotencyKey("abc"))
                .thenReturn(Optional.of(existing));

        TransferResponse response =
                transferService.transfer(null, "abc");

        assertEquals(
                existing.getId(),
                response.transferId());
    }
}
//package com.assignment.wallet_transfer.service.impltest;
//
//import com.assignment.wallet_transfer.dtos.TransferRequest;
//import com.assignment.wallet_transfer.dtos.TransferResponse;
//import com.assignment.wallet_transfer.entity.LedgerEntry;
//import com.assignment.wallet_transfer.entity.Transfer;
//import com.assignment.wallet_transfer.entity.Wallet;
//import com.assignment.wallet_transfer.enums.TransferStatus;
//import com.assignment.wallet_transfer.exception.InsufficientBalanceException;
//import com.assignment.wallet_transfer.repository.LedgerRepository;
//import com.assignment.wallet_transfer.repository.TransferRepository;
//import com.assignment.wallet_transfer.repository.WalletRepository;
//import com.assignment.wallet_transfer.service.impl.TransferServiceImpl;
//import org.junit.jupiter.api.Assertions;
//import org.junit.jupiter.api.Test;
//import org.junit.jupiter.api.extension.ExtendWith;
//import org.mockito.InjectMocks;
//import org.mockito.Mock;
//import org.springframework.test.context.junit.jupiter.SpringExtension;
//
//import java.math.BigDecimal;
//import java.util.Optional;
//import java.util.UUID;
//
//import static org.junit.jupiter.api.Assertions.assertEquals;
//import static org.junit.jupiter.api.Assertions.assertThrows;
//import static org.mockito.Mockito.*;
//
//@ExtendWith(SpringExtension.class)
//public class TransferServiceImplTest {
//
//    @InjectMocks
//    private TransferServiceImpl transferService;
//
//    @Mock
//    private TransferRepository transferRepository;
//
//    @Mock
//    private WalletRepository walletRepository;
//
//    @Mock
//    private LedgerRepository ledgerRepository;
//
//    @Test
//    void shouldTransferMoneySuccessfully() {
//
//        Wallet source = Wallet.builder()
//                .id(UUID.randomUUID())
//                .balance(BigDecimal.valueOf(1000))
//                .build();
//
//        Wallet destination = Wallet.builder()
//                .id(UUID.randomUUID())
//                .balance(BigDecimal.valueOf(500))
//                .build();
//
//        when(transferRepository.findByIdempotencyKey("abc"))
//                .thenReturn(Optional.empty());
//
//        when(walletRepository.findById(source.getId()))
//                .thenReturn(Optional.of(source));
//
//        when(walletRepository.findById(destination.getId()))
//                .thenReturn(Optional.of(destination));
//
//        TransferRequest request =
//                new TransferRequest(
//                        source.getId(),
//                        destination.getId(),
//                        BigDecimal.valueOf(100)
//                );
//
//        TransferResponse response =
//                transferService.transfer(request, "abc");
//
//        assertEquals(
//                BigDecimal.valueOf(900),
//                source.getBalance());
//
//        assertEquals(
//                BigDecimal.valueOf(600),
//                destination.getBalance());
//
//        assertEquals(
//                TransferStatus.PROCESSED,
//                response.status());
//    }
//
//    @Test
//    void shouldRejectTransferWhenBalanceInsufficient() {
//
//        Wallet source = Wallet.builder()
//                .id(UUID.randomUUID())
//                .balance(BigDecimal.valueOf(10))
//                .build();
//
//        Wallet destination = Wallet.builder()
//                .id(UUID.randomUUID())
//                .balance(BigDecimal.valueOf(500))
//                .build();
//
//        when(transferRepository.findByIdempotencyKey("abc"))
//                .thenReturn(Optional.empty());
//
//        when(walletRepository.findById(source.getId()))
//                .thenReturn(Optional.of(source));
//
//        when(walletRepository.findById(destination.getId()))
//                .thenReturn(Optional.of(destination));
//
//        TransferRequest request =
//                new TransferRequest(
//                        source.getId(),
//                        destination.getId(),
//                        BigDecimal.valueOf(100)
//                );
//
//        assertThrows(
//                InsufficientBalanceException.class,
//                () -> transferService.transfer(request, "abc")
//        );
//    }
//
//    @Test
//    void shouldReturnExistingTransferForDuplicateIdempotencyKey() {
//
//        Transfer existing =
//                Transfer.builder()
//                        .id(UUID.randomUUID())
//                        .amount(BigDecimal.valueOf(100))
//                        .status(TransferStatus.PROCESSED)
//                        .build();
//
//        when(transferRepository.findByIdempotencyKey("abc"))
//                .thenReturn(Optional.of(existing));
//
//        TransferResponse response =
//                transferService.transfer(null, "abc");
//
//        assertEquals(
//                existing.getId(),
//                response.transferId());
//    }
//
//    @Test
//    void shouldCreateDebitAndCreditLedgerEntries() {
//
//        // after transfer
//
//        verify(ledgerRepository, times(2))
//                .save(any(LedgerEntry.class));
//    }
//}
