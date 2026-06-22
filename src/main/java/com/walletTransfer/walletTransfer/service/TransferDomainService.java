package com.walletTransfer.walletTransfer.service;

import com.walletTransfer.walletTransfer.entity.LedgerEntry;
import com.walletTransfer.walletTransfer.entity.Transfer;
import com.walletTransfer.walletTransfer.entity.Wallet;
import com.walletTransfer.walletTransfer.dto.TransferRequest;
import com.walletTransfer.walletTransfer.dto.TransferResponse;

import java.util.List;

public interface TransferDomainService {

    List<LedgerEntry> processTransfer(Wallet sourceWallet, Wallet destinationWallet, Transfer transfer);
}
