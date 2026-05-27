package com.wallettransfer.web;

import com.wallettransfer.service.WalletService;
import com.wallettransfer.web.dto.CreateWalletRequest;
import com.wallettransfer.web.dto.WalletResponse;
import jakarta.validation.Valid;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.ResponseStatus;
import org.springframework.web.bind.annotation.RestController;

/**
 * HTTP API for creating wallets and reading balances (supporting/demo endpoints).
 */
@RestController
@RequestMapping("/wallets")
public class WalletController {

    private final WalletService walletService;

    /**
     * @param walletService wallet business operations
     */
    public WalletController(WalletService walletService) {
        this.walletService = walletService;
    }

    /**
     * Creates a wallet with an initial balance.
     *
     * @param request wallet id and initial balance
     * @return created wallet
     */
    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    public WalletResponse createWallet(@Valid @RequestBody CreateWalletRequest request) {
        return walletService.createWallet(request);
    }

    /**
     * Returns the current balance for a wallet.
     *
     * @param walletId wallet identifier
     * @return wallet details including balance
     */
    @GetMapping("/{walletId}")
    public WalletResponse getWallet(@PathVariable String walletId) {
        return walletService.getWallet(walletId);
    }
}
