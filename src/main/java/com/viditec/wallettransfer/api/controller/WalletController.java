package com.viditec.wallettransfer.api.controller;

import com.viditec.wallettransfer.api.request.CreateWalletRequest;
import com.viditec.wallettransfer.api.response.WalletResponse;
import com.viditec.wallettransfer.mapper.ApiMapper;
import com.viditec.wallettransfer.service.WalletService;
import jakarta.validation.Valid;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.ResponseStatus;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/wallets")
public class WalletController {

    private final WalletService walletService;
    private final ApiMapper apiMapper;

    public WalletController(WalletService walletService, ApiMapper apiMapper) {
        this.walletService = walletService;
        this.apiMapper = apiMapper;
    }

    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    public WalletResponse createWallet(@Valid @RequestBody CreateWalletRequest request) {
        return apiMapper.toResponse(walletService.createWallet(apiMapper.toDto(request)));
    }

    @GetMapping("/{walletId}")
    public WalletResponse getWallet(@PathVariable String walletId) {
        return apiMapper.toResponse(walletService.getWallet(walletId));
    }
}
