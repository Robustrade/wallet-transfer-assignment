package com.wallet.controller;

import com.wallet.domain.Transfer;
import com.wallet.domain.Wallet;
import com.wallet.dto.LedgerEntryView;
import com.wallet.service.TransferService;
import com.wallet.dto.CreateTransferRequest;
import com.wallet.dto.TransferResponse;
import jakarta.validation.Valid;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.util.List;
import java.util.Map;
import java.util.UUID;


@RestController
@RequestMapping
public class TransferController {

    private final TransferService transferService;

    public TransferController(TransferService transferService) {
        this.transferService = transferService;
    }

    @PostMapping("/transfers")
    public ResponseEntity<TransferResponse> createTransfer(@Valid @RequestBody CreateTransferRequest request) {
        TransferResponse response = transferService.createTransfer(request);
        HttpStatus status = response.replayed() ? HttpStatus.OK : HttpStatus.CREATED;
        return ResponseEntity.status(status).body(response);
    }

    @GetMapping("/transfers/{id}")
    public ResponseEntity<TransferResponse> getTransfer(@PathVariable UUID id) {
        Transfer transfer = transferService.getTransfer(id);
        return ResponseEntity.ok(TransferResponse.from(transfer, false));
    }

    @GetMapping("/wallets/{id}")
    public ResponseEntity<Map<String, Object>> getWallet(@PathVariable String id) {
        Wallet wallet = transferService.getWallet(id);
        return ResponseEntity.ok(Map.of(
                "walletId", wallet.getId(),
                "balance", wallet.getBalance()
        ));
    }

    @GetMapping("/wallets/{id}/ledger")
    public ResponseEntity<List<LedgerEntryView>> getWalletLedger(@PathVariable String id) {
        // Touch the wallet first so a bad id yields a 404 instead of an empty ledger.
        transferService.getWallet(id);
        List<LedgerEntryView> entries = transferService.getWalletLedger(id).stream()
                .map(LedgerEntryView::from)
                .toList();
        return ResponseEntity.ok(entries);
    }

}
