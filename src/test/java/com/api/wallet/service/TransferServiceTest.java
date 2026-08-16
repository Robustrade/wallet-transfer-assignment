package com.api.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.api.wallet.domain.IdempotencyRecord;
import com.api.wallet.domain.LedgerEntry;
import com.api.wallet.domain.Transfer;
import com.api.wallet.domain.TransferStatus;
import com.api.wallet.domain.Wallet;
import com.api.wallet.dto.CreateTransferRequest;
import com.api.wallet.dto.TransferResponse;
import com.api.wallet.repository.IdempotencyRecordRepository;
import com.api.wallet.repository.LedgerEntryRepository;
import com.api.wallet.repository.TransferRepository;
import com.api.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import java.util.Optional;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.dao.DataIntegrityViolationException;
import tools.jackson.databind.json.JsonMapper;

@ExtendWith(MockitoExtension.class)
class TransferServiceTest {

    @Mock private WalletRepository walletRepository;
    @Mock private TransferRepository transferRepository;
    @Mock private LedgerEntryRepository ledgerEntryRepository;
    @Mock private IdempotencyRecordRepository idempotencyRecordRepository;

    private TransferService transferService;

    @BeforeEach
    void setUp() {
        TransferAttemptExecutor transferAttemptExecutor =
                new TransferAttemptExecutor(
                        walletRepository,
                        transferRepository,
                        ledgerEntryRepository,
                        idempotencyRecordRepository,
                        JsonMapper.builder().build());
        transferService =
                new TransferService(
                        walletRepository,
                        transferRepository,
                        idempotencyRecordRepository,
                        JsonMapper.builder().build(),
                        transferAttemptExecutor);
    }

    private static Wallet walletWithBalance(String id, String balance) {
        return new Wallet(id, new BigDecimal(balance));
    }

    private IdempotencyRecord runToCompletion(CreateTransferRequest request, Wallet fromWallet, Wallet toWallet) {
        when(idempotencyRecordRepository.findById(request.idempotencyKey())).thenReturn(Optional.empty());
        when(walletRepository.existsById(request.fromWalletId())).thenReturn(true);
        when(walletRepository.existsById(request.toWalletId())).thenReturn(true);
        when(walletRepository.findByIdForUpdate(request.fromWalletId())).thenReturn(Optional.of(fromWallet));
        when(walletRepository.findByIdForUpdate(request.toWalletId())).thenReturn(Optional.of(toWallet));

        transferService.createTransfer(request);

        ArgumentCaptor<IdempotencyRecord> captor = ArgumentCaptor.forClass(IdempotencyRecord.class);
        verify(idempotencyRecordRepository).save(captor.capture());
        return captor.getValue();
    }

    @Test
    void replaysWinnersResponseWhenLosingTheConcurrentIdempotencyKeyInsertRace() {
        CreateTransferRequest request =
                new CreateTransferRequest("key-6", "wallet_1", "wallet_2", new BigDecimal("100"));
        Wallet fromWallet = walletWithBalance("wallet_1", "1000");
        Wallet toWallet = walletWithBalance("wallet_2", "500");
        IdempotencyRecord winnerRecord = runToCompletion(request, fromWallet, toWallet);

        when(idempotencyRecordRepository.findById("key-6"))
                .thenReturn(Optional.empty())
                .thenReturn(Optional.of(winnerRecord));
        when(transferRepository.saveAndFlush(any(Transfer.class)))
                .thenThrow(new DataIntegrityViolationException("duplicate key"));

        TransferResponse replayed = transferService.createTransfer(request);

        assertThat(replayed.status()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(fromWallet.getBalance()).isEqualByComparingTo("900");
        verify(ledgerEntryRepository, times(2)).save(any(LedgerEntry.class));
    }
}
