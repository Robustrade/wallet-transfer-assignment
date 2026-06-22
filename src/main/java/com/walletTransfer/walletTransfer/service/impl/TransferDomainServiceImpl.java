package com.walletTransfer.walletTransfer.service.impl;


import com.walletTransfer.walletTransfer.entity.LedgerEntry;
    import com.walletTransfer.walletTransfer.entity.Transfer;
import com.walletTransfer.walletTransfer.entity.Wallet;
import com.walletTransfer.walletTransfer.exception.InsufficientBalanceException;
import com.walletTransfer.walletTransfer.factory.LedgerEntryFactory;
import com.walletTransfer.walletTransfer.service.TransferDomainService;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;

import java.util.List;

@Service
@RequiredArgsConstructor
public class TransferDomainServiceImpl  implements  TransferDomainService {
    private final LedgerEntryFactory ledgerEntryFactory;

    @Override
    public List<LedgerEntry> processTransfer(Wallet sourceWallet, Wallet destinationWallet, Transfer transfer) {

        validateBalance(sourceWallet, transfer.getAmount());

        sourceWallet.debit(transfer.getAmount());

        destinationWallet.credit(transfer.getAmount());

        LedgerEntry debitEntry = ledgerEntryFactory.createDebitEntry(
                        sourceWallet,
                        transfer,
                        transfer.getAmount());

        LedgerEntry creditEntry = ledgerEntryFactory.createCreditEntry(
                        destinationWallet,
                        transfer,
                        transfer.getAmount());

        transfer.markProcessed();

        return List.of(
                debitEntry,
                creditEntry);
    }

    private void validateBalance(
            Wallet wallet,
            Long amount) {

        if (!wallet.hasSufficientBalance(amount)) {

            throw new InsufficientBalanceException(
                    "Insufficient balance in wallet: "
                            + wallet.getId());
        }
    }
}
