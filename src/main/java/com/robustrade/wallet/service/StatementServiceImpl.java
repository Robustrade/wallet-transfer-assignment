package com.robustrade.wallet.service;

import com.robustrade.wallet.domain.LedgerEntry;
import com.robustrade.wallet.domain.TransactionType;
import com.robustrade.wallet.domain.Wallet;
import com.robustrade.wallet.handler.dto.StatementEntryResponse;
import com.robustrade.wallet.handler.dto.StatementResponse;
import com.robustrade.wallet.repository.LedgerEntryRepository;
import com.robustrade.wallet.repository.WalletRepository;
import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Slf4j
@Service
@RequiredArgsConstructor
public class StatementServiceImpl implements StatementService {

    private final WalletRepository walletRepository;
    private final LedgerEntryRepository ledgerEntryRepository;

    @Override
    @Transactional
    public StatementResponse getStatement(Long userId, String walletId) {
        log.info("Building statement for userId={}, walletId={}", userId, walletId);

        try {
            // Lock wallet for the duration of this TX so balance + ledger reads are consistent
            // under concurrent transfers (avoids READ_COMMITTED split view).
            Wallet wallet = resolveWallet(userId, walletId);
            log.debug(
                    "Resolved wallet for statement: walletId={}, userId={}, currentBalance={}",
                    wallet.getId(),
                    wallet.getUserId(),
                    wallet.getBalance()
            );

            List<LedgerEntry> ledgerEntries =
                    ledgerEntryRepository.findByWalletIdOrderByCreatedAtAscIdAsc(wallet.getId());
            log.debug(
                    "Fetched {} ledger entries for walletId={}",
                    ledgerEntries.size(),
                    wallet.getId()
            );

            BigDecimal openingBalance = computeOpeningBalance(wallet.getBalance(), ledgerEntries);
            log.debug("Computed opening balance={} for walletId={}", openingBalance, wallet.getId());

            List<StatementEntryResponse> entries = buildEntries(openingBalance, ledgerEntries);

            StatementResponse response = StatementResponse.builder()
                    .walletId(wallet.getId())
                    .userId(wallet.getUserId())
                    .currentBalance(wallet.getBalance())
                    .entries(entries)
                    .build();

            log.info(
                    "Statement built successfully: walletId={}, userId={}, currentBalance={}, entries={}",
                    response.getWalletId(),
                    response.getUserId(),
                    response.getCurrentBalance(),
                    entries.size()
            );
            return response;
        } catch (Exception ex) {
            log.error(
                    "Error while building statement for userId={}, walletId={}: {}",
                    userId,
                    walletId,
                    ex.getMessage(),
                    ex
            );
            throw ex;
        }
    }

    private Wallet resolveWallet(Long userId, String walletId) {
        boolean hasUserId = userId != null;
        boolean hasWalletId = walletId != null && !walletId.isBlank();

        if (!hasUserId && !hasWalletId) {
            log.error("Statement lookup missing both userId and walletId");
            throw WalletServiceException.missingLookup();
        }

        if (hasWalletId) {
            log.debug("Resolving wallet by walletId={} (FOR UPDATE)", walletId);
            Wallet wallet = walletRepository.findByIdForUpdate(walletId)
                    .orElseThrow(WalletServiceException::walletNotFound);
            if (hasUserId && !wallet.getUserId().equals(userId)) {
                log.error(
                        "walletId={} does not belong to userId={}, actualUserId={}",
                        walletId,
                        userId,
                        wallet.getUserId()
                );
                throw WalletServiceException.walletUserMismatch();
            }
            return wallet;
        }

        log.debug("Resolving wallet by userId={} then locking row", userId);
        List<Wallet> wallets = walletRepository.findByUserId(userId);
        if (wallets.isEmpty()) {
            log.error("No wallet found for userId={}", userId);
            throw WalletServiceException.walletNotFound();
        }
        if (wallets.size() > 1) {
            log.error("Multiple wallets found for userId={}, count={}", userId, wallets.size());
            throw WalletServiceException.multipleWalletsForUser();
        }
        return walletRepository.findByIdForUpdate(wallets.get(0).getId())
                .orElseThrow(WalletServiceException::walletNotFound);
    }

    private static BigDecimal computeOpeningBalance(BigDecimal currentBalance, List<LedgerEntry> entries) {
        BigDecimal opening = currentBalance;
        for (LedgerEntry entry : entries) {
            if (entry.getTransactionType() == TransactionType.CREDIT) {
                opening = opening.subtract(entry.getAmount());
            } else {
                opening = opening.add(entry.getAmount());
            }
        }
        return opening;
    }

    private static List<StatementEntryResponse> buildEntries(
            BigDecimal openingBalance,
            List<LedgerEntry> ledgerEntries
    ) {
        List<StatementEntryResponse> entries = new ArrayList<>();
        BigDecimal running = openingBalance;

        for (LedgerEntry entry : ledgerEntries) {
            BigDecimal previous = running;
            if (entry.getTransactionType() == TransactionType.CREDIT) {
                running = running.add(entry.getAmount());
            } else {
                running = running.subtract(entry.getAmount());
            }

            entries.add(StatementEntryResponse.builder()
                    .transferId(entry.getTransferId())
                    .transferType(entry.getTransactionType())
                    .amount(entry.getAmount())
                    .previousBalance(previous)
                    .balanceAfterTransfer(running)
                    .transferDate(entry.getCreatedAt())
                    .build());
        }

        return entries;
    }
}
