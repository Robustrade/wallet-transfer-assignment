package com.robustrade.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.robustrade.wallet.domain.LedgerEntry;
import com.robustrade.wallet.domain.TransactionType;
import com.robustrade.wallet.domain.Wallet;
import com.robustrade.wallet.handler.dto.CreateWalletRequest;
import com.robustrade.wallet.handler.dto.WalletResponse;
import com.robustrade.wallet.repository.LedgerEntryRepository;
import com.robustrade.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.ArgumentCaptor;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.dao.DataIntegrityViolationException;

@ExtendWith(MockitoExtension.class)
class WalletServiceImplTest {

    @Mock
    private WalletRepository walletRepository;

    @Mock
    private LedgerEntryRepository ledgerEntryRepository;

    @InjectMocks
    private WalletServiceImpl walletService;

    @Test
    void createWallet_whenUserHasNoWallet_savesFundingLedgerAndReturnsResponse() {
        CreateWalletRequest request = CreateWalletRequest.builder()
                .userId(1001L)
                .balance(new BigDecimal("1000.00"))
                .build();

        when(walletRepository.existsByUserId(1001L)).thenReturn(false);
        when(walletRepository.saveAndFlush(any(Wallet.class))).thenAnswer(invocation -> invocation.getArgument(0));
        when(ledgerEntryRepository.save(any(LedgerEntry.class))).thenAnswer(invocation -> invocation.getArgument(0));

        WalletResponse response = walletService.createWallet(request);

        assertThat(response.getUserId()).isEqualTo(1001L);
        assertThat(response.getBalance()).isEqualByComparingTo("1000.00");
        assertThat(response.getWalletId()).isNotBlank();
        assertThat(response.getCreatedAt()).isNotNull();

        ArgumentCaptor<Wallet> walletCaptor = ArgumentCaptor.forClass(Wallet.class);
        verify(walletRepository).existsByUserId(1001L);
        verify(walletRepository).saveAndFlush(walletCaptor.capture());

        Wallet saved = walletCaptor.getValue();
        assertThat(saved.getUserId()).isEqualTo(1001L);
        assertThat(saved.getBalance()).isEqualByComparingTo("1000.00");
        assertThat(saved.getId()).isEqualTo(response.getWalletId());

        ArgumentCaptor<LedgerEntry> ledgerCaptor = ArgumentCaptor.forClass(LedgerEntry.class);
        verify(ledgerEntryRepository).save(ledgerCaptor.capture());
        LedgerEntry funding = ledgerCaptor.getValue();
        assertThat(funding.getWalletId()).isEqualTo(response.getWalletId());
        assertThat(funding.getTransferId()).isNull();
        assertThat(funding.getTransactionType()).isEqualTo(TransactionType.CREDIT);
        assertThat(funding.getAmount()).isEqualByComparingTo("1000.00");
    }

    @Test
    void createWallet_whenZeroBalance_doesNotWriteFundingLedger() {
        CreateWalletRequest request = CreateWalletRequest.builder()
                .userId(1002L)
                .balance(BigDecimal.ZERO)
                .build();

        when(walletRepository.existsByUserId(1002L)).thenReturn(false);
        when(walletRepository.saveAndFlush(any(Wallet.class))).thenAnswer(invocation -> invocation.getArgument(0));

        walletService.createWallet(request);

        verify(ledgerEntryRepository, never()).save(any(LedgerEntry.class));
    }

    @Test
    void createWallet_whenWalletAlreadyExists_throwsConflict() {
        CreateWalletRequest request = CreateWalletRequest.builder()
                .userId(1001L)
                .balance(new BigDecimal("500.00"))
                .build();

        when(walletRepository.existsByUserId(1001L)).thenReturn(true);

        assertThatThrownBy(() -> walletService.createWallet(request))
                .isInstanceOf(WalletServiceException.class)
                .satisfies(ex -> {
                    WalletServiceException serviceException = (WalletServiceException) ex;
                    assertThat(serviceException.getErrorCode()).isEqualTo("WALLET_ALREADY_EXISTS");
                    assertThat(serviceException.getHttpStatus()).isEqualTo(409);
                    assertThat(serviceException.getMessage()).contains("1001");
                });

        verify(walletRepository).existsByUserId(1001L);
        verify(walletRepository, never()).saveAndFlush(any(Wallet.class));
        verify(ledgerEntryRepository, never()).save(any(LedgerEntry.class));
    }

    @Test
    void createWallet_whenUniqueUserIdRace_mapsToConflict() {
        CreateWalletRequest request = CreateWalletRequest.builder()
                .userId(1001L)
                .balance(new BigDecimal("500.00"))
                .build();

        when(walletRepository.existsByUserId(1001L)).thenReturn(false);
        when(walletRepository.saveAndFlush(any(Wallet.class))).thenThrow(
                new DataIntegrityViolationException(
                        "Unique index or primary key violation: wallets_user_id_uk"
                )
        );

        assertThatThrownBy(() -> walletService.createWallet(request))
                .isInstanceOf(WalletServiceException.class)
                .satisfies(ex -> {
                    WalletServiceException serviceException = (WalletServiceException) ex;
                    assertThat(serviceException.getErrorCode()).isEqualTo("WALLET_ALREADY_EXISTS");
                    assertThat(serviceException.getHttpStatus()).isEqualTo(409);
                });

        verify(walletRepository).saveAndFlush(any(Wallet.class));
        verify(ledgerEntryRepository, never()).save(any(LedgerEntry.class));
    }
}
