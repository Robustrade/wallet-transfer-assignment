package com.example.wallet.handler;

import com.example.wallet.service.TransferService;
import com.example.wallet.service.WalletService;
import jakarta.validation.Valid;
import java.util.List;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.ResponseStatus;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class WalletController {

    private static final int MAX_HISTORY = 100;

    private final WalletService walletService;
    private final TransferService transferService;

    public WalletController(WalletService walletService, TransferService transferService) {
        this.walletService = walletService;
        this.transferService = transferService;
    }

    @PostMapping("/wallets")
    @ResponseStatus(HttpStatus.CREATED)
    public WalletResponse create(@Valid @RequestBody CreateWalletRequest req) {
        return WalletResponse.from(walletService.createWallet(req.id(), req.openingBalance()));
    }

    @GetMapping("/wallets/{id}")
    public WalletResponse get(@PathVariable String id) {
        return WalletResponse.from(walletService.getWallet(id));
    }

    @GetMapping("/wallets/{id}/transfers")
    public List<TransferResponse> history(
            @PathVariable String id, @RequestParam(defaultValue = "50") int limit) {
        int bounded = Math.max(1, Math.min(limit, MAX_HISTORY));
        return transferService.getTransfersForWallet(id, bounded).stream()
                .map(TransferResponse::from)
                .toList();
    }
}
