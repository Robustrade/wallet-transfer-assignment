package com.wallettransfer.wallet.dto;

import com.wallettransfer.ledger.dto.LedgerEntryResponse;
import com.wallettransfer.transfer.dto.TransferResponse;
import java.util.List;

public record WalletHistoryResponse(
        String walletId,
        long balance,
        long ledgerBalance,
        long totalDebits,
        long totalCredits,
        int processedTransfers,
        int failedTransfers,
        List<TransferResponse> transfers,
        List<LedgerEntryResponse> ledgerEntries,
        List<TransactionDayStat> dailyStats
) {
}
