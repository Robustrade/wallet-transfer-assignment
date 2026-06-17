package com.wallettransfer.wallet.service;

import com.wallettransfer.ledger.domain.LedgerEntry;
import com.wallettransfer.ledger.domain.LedgerEntryType;
import com.wallettransfer.ledger.dto.LedgerEntryResponse;
import com.wallettransfer.ledger.repository.LedgerEntryRepository;
import com.wallettransfer.transfer.domain.Transfer;
import com.wallettransfer.transfer.domain.TransferStatus;
import com.wallettransfer.transfer.dto.TransferResponse;
import com.wallettransfer.transfer.repository.TransferRepository;
import com.wallettransfer.wallet.domain.InsufficientBalanceException;
import com.wallettransfer.wallet.domain.Wallet;
import com.wallettransfer.wallet.dto.CreateWalletRequest;
import com.wallettransfer.wallet.dto.TransactionDayStat;
import com.wallettransfer.wallet.dto.WalletHistoryResponse;
import com.wallettransfer.wallet.dto.WalletResponse;
import com.wallettransfer.wallet.repository.WalletRepository;
import java.time.LocalDate;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.function.Function;
import java.util.stream.Collectors;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class WalletService {

    private static final int CHART_DAYS = 90;

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;

    public WalletService(
            WalletRepository walletRepository,
            TransferRepository transferRepository,
            LedgerEntryRepository ledgerEntryRepository) {
        this.walletRepository = walletRepository;
        this.transferRepository = transferRepository;
        this.ledgerEntryRepository = ledgerEntryRepository;
    }

    @Transactional(readOnly = true)
    public List<WalletResponse> listWallets() {
        return walletRepository.findAll().stream()
                .sorted(Comparator.comparing(Wallet::getId))
                .map(wallet -> new WalletResponse(wallet.getId(), wallet.getBalance()))
                .toList();
    }

    @Transactional
    public WalletResponse createWallet(CreateWalletRequest request) {
        if (walletRepository.existsById(request.id())) {
            throw new WalletAlreadyExistsException(request.id());
        }

        try {
            Wallet wallet = walletRepository.save(new Wallet(request.id(), request.initialBalance()));
            return new WalletResponse(wallet.getId(), wallet.getBalance());
        } catch (DataIntegrityViolationException ex) {
            throw new WalletAlreadyExistsException(request.id());
        }
    }

    @Transactional(readOnly = true)
    public WalletHistoryResponse getWalletHistory(String walletId) {
        Wallet wallet = walletRepository
                .findById(walletId)
                .orElseThrow(() -> new WalletNotFoundException(walletId));

        List<LedgerEntry> ledgerEntries = ledgerEntryRepository.findByWalletIdOrderByCreatedAtDesc(walletId);
        long totalDebits = sumByType(ledgerEntries, LedgerEntryType.DEBIT);
        long totalCredits = sumByType(ledgerEntries, LedgerEntryType.CREDIT);
        long ledgerBalance = totalCredits - totalDebits;

        List<Transfer> transfers = transferRepository.findByWalletInvolvement(walletId);
        int processedTransfers = countByStatus(transfers, TransferStatus.PROCESSED);
        int failedTransfers = countByStatus(transfers, TransferStatus.FAILED);

        return new WalletHistoryResponse(
                wallet.getId(),
                wallet.getBalance(),
                ledgerBalance,
                totalDebits,
                totalCredits,
                processedTransfers,
                failedTransfers,
                transfers.stream().map(this::toTransferResponse).toList(),
                ledgerEntries.stream().map(this::toLedgerResponse).toList(),
                buildDailyStats(transfers));
    }

    @Transactional(readOnly = true)
    public void ensureWalletsExist(String fromWalletId, String toWalletId) {
        if (!walletRepository.existsById(fromWalletId)) {
            throw new WalletNotFoundException(fromWalletId);
        }
        if (!walletRepository.existsById(toWalletId)) {
            throw new WalletNotFoundException(toWalletId);
        }
    }

    @Transactional(noRollbackFor = InsufficientBalanceException.class)
    public void moveFunds(String fromWalletId, String toWalletId, long amount) {
        List<String> walletIds = List.of(fromWalletId, toWalletId).stream().sorted().toList();

        Map<String, Wallet> wallets = walletRepository.findByIdsForUpdate(walletIds).stream()
                .collect(Collectors.toMap(Wallet::getId, Function.identity()));

        Wallet fromWallet = wallets.get(fromWalletId);
        Wallet toWallet = wallets.get(toWalletId);
        if (fromWallet == null || toWallet == null) {
            throw new IllegalStateException("Wallet missing during transfer processing");
        }

        fromWallet.debit(amount);
        toWallet.credit(amount);
    }

    private long sumByType(List<LedgerEntry> entries, LedgerEntryType type) {
        return entries.stream()
                .filter(entry -> entry.getEntryType() == type)
                .mapToLong(LedgerEntry::getAmount)
                .sum();
    }

    private int countByStatus(List<Transfer> transfers, TransferStatus status) {
        return (int) transfers.stream().filter(transfer -> transfer.getStatus() == status).count();
    }

    private List<TransactionDayStat> buildDailyStats(List<Transfer> transfers) {
        LocalDate end = LocalDate.now(ZoneOffset.UTC);
        LocalDate start = end.minusDays(CHART_DAYS - 1L);

        Map<LocalDate, long[]> grouped = new HashMap<>();
        for (Transfer transfer : transfers) {
            if (transfer.getStatus() != TransferStatus.PROCESSED) {
                continue;
            }
            LocalDate day = transfer.getCreatedAt().atZone(ZoneOffset.UTC).toLocalDate();
            if (day.isBefore(start) || day.isAfter(end)) {
                continue;
            }
            long[] stats = grouped.computeIfAbsent(day, ignored -> new long[2]);
            stats[0]++;
            stats[1] += transfer.getAmount();
        }

        List<TransactionDayStat> result = new ArrayList<>(CHART_DAYS);
        for (LocalDate day = start; !day.isAfter(end); day = day.plusDays(1)) {
            long[] stats = grouped.getOrDefault(day, new long[] {0, 0});
            result.add(new TransactionDayStat(day.toString(), (int) stats[0], stats[1]));
        }
        return result;
    }

    private TransferResponse toTransferResponse(Transfer transfer) {
        return new TransferResponse(
                transfer.getId(),
                null,
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount(),
                transfer.getStatus(),
                transfer.getFailureReason(),
                false,
                transfer.getCreatedAt());
    }

    private LedgerEntryResponse toLedgerResponse(LedgerEntry entry) {
        return new LedgerEntryResponse(
                entry.getId(),
                entry.getTransferId(),
                entry.getWalletId(),
                entry.getEntryType(),
                entry.getAmount(),
                entry.getCreatedAt());
    }
}
