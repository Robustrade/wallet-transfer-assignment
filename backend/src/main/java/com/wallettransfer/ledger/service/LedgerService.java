package com.wallettransfer.ledger.service;

import com.wallettransfer.ledger.domain.LedgerEntry;
import com.wallettransfer.ledger.domain.LedgerEntryType;
import com.wallettransfer.ledger.dto.LedgerEntryResponse;
import com.wallettransfer.ledger.repository.LedgerEntryRepository;
import com.wallettransfer.transfer.domain.Transfer;
import java.util.List;
import java.util.UUID;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class LedgerService {

    private final LedgerEntryRepository ledgerEntryRepository;

    public LedgerService(LedgerEntryRepository ledgerEntryRepository) {
        this.ledgerEntryRepository = ledgerEntryRepository;
    }

    @Transactional
    public void recordTransfer(Transfer transfer) {
        ledgerEntryRepository.save(new LedgerEntry(
                transfer.getId(), transfer.getFromWalletId(), LedgerEntryType.DEBIT, transfer.getAmount()));
        ledgerEntryRepository.save(new LedgerEntry(
                transfer.getId(), transfer.getToWalletId(), LedgerEntryType.CREDIT, transfer.getAmount()));
    }

    @Transactional(readOnly = true)
    public List<LedgerEntryResponse> listByTransferId(UUID transferId) {
        return ledgerEntryRepository.findByTransferIdOrderByIdAsc(transferId).stream()
                .map(entry -> new LedgerEntryResponse(
                        entry.getId(),
                        entry.getTransferId(),
                        entry.getWalletId(),
                        entry.getEntryType(),
                        entry.getAmount(),
                        entry.getCreatedAt()))
                .toList();
    }
}
