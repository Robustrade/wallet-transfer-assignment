package com.wallet.transfer.controller;

import com.wallet.transfer.domain.LedgerEntry;
import com.wallet.transfer.repository.LedgerEntryRepository;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;
import java.util.List;
import java.util.Map;

@RestController
@RequestMapping("/ledger")
public class LedgerController {

    private final LedgerEntryRepository ledgerEntryRepository;

    public LedgerController(LedgerEntryRepository ledgerEntryRepository) {
        this.ledgerEntryRepository = ledgerEntryRepository;
    }

    @GetMapping
    public ResponseEntity<List<LedgerEntry>> getAllEntries() {
        return ResponseEntity.ok(ledgerEntryRepository.findAll());
    }

    @GetMapping("/transfer/{transferId}")
    public ResponseEntity<List<LedgerEntry>> getEntriesByTransfer(@PathVariable String transferId) {
        return ResponseEntity.ok(ledgerEntryRepository.findByTransferId(transferId));
    }

    @GetMapping("/wallet/{walletId}")
    public ResponseEntity<List<LedgerEntry>> getEntriesByWallet(@PathVariable String walletId) {
        return ResponseEntity.ok(ledgerEntryRepository.findByWalletId(walletId));
    }

    @GetMapping("/balance-check")
    public ResponseEntity<Map<String, Object>> balanceCheck() {
        Long totalDebits = java.util.Objects.requireNonNullElse(ledgerEntryRepository.sumDebits(), 0L);
        Long totalCredits = java.util.Objects.requireNonNullElse(ledgerEntryRepository.sumCredits(), 0L);
        boolean balanced = totalDebits.equals(totalCredits);
        return ResponseEntity.ok(Map.of(
                "totalDebits", totalDebits,
                "totalCredits", totalCredits,
                "balanced", balanced
        ));
    }
}
