package com.wallet.controller;

import com.wallet.domain.LedgerEntry;
import com.wallet.dto.WalletResponse;
import com.wallet.service.WalletService;
import java.util.List;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/wallets")
public class WalletController {

    private final WalletService walletService;

    public WalletController(WalletService walletService) {
        this.walletService = walletService;
    }

    @GetMapping("/{id}")
    public ResponseEntity<WalletResponse> getWallet(@PathVariable String id) {
        return ResponseEntity.ok(walletService.getWallet(id));
    }

    @GetMapping("/{id}/ledger")
    public ResponseEntity<List<LedgerEntry>> getLedger(@PathVariable String id) {
        return ResponseEntity.ok(walletService.getLedgerEntries(id));
    }
}
