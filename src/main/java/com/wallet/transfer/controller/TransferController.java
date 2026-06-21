package com.wallet.transfer.controller;

import com.wallet.transfer.dto.TransferRequest;
import com.wallet.transfer.dto.TransferResponse;
import com.wallet.transfer.dto.TransferResult;
import com.wallet.transfer.dto.WalletResponse;
import com.wallet.transfer.service.TransferService;
import com.wallet.transfer.service.WalletQueryService;
import jakarta.validation.Valid;
import jakarta.validation.constraints.Size;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.validation.annotation.Validated;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.servlet.support.ServletUriComponentsBuilder;

import java.net.URI;

@RestController
@Validated
public class TransferController {

    private final TransferService transferService;
    private final WalletQueryService walletQueryService;

    public TransferController(TransferService transferService,
                              WalletQueryService walletQueryService) {
        this.transferService = transferService;
        this.walletQueryService = walletQueryService;
    }

    @PostMapping(value = "/transfers", consumes = MediaType.APPLICATION_JSON_VALUE)
    public ResponseEntity<TransferResponse> createTransfer(@Valid @RequestBody TransferRequest request) {
        TransferResult result = transferService.executeTransfer(
                request.idempotencyKey(),
                request.fromWalletId(),
                request.toWalletId(),
                request.amount()
        );

        TransferResponse response = TransferResponse.from(result.transfer());
        if (result.created()) {
            URI location = ServletUriComponentsBuilder.fromCurrentRequest()
                    .path("/{id}")
                    .buildAndExpand(result.transfer().getId())
                    .toUri();
            return ResponseEntity.created(location).body(response);
        }
        return ResponseEntity.ok(response);
    }

    @GetMapping("/transfers/{transferId}")
    public ResponseEntity<TransferResponse> getTransfer(
            @PathVariable @Size(max = 64) String transferId) {
        return walletQueryService.getTransfer(transferId)
                .map(t -> ResponseEntity.ok(TransferResponse.from(t)))
                .orElse(ResponseEntity.notFound().build());
    }

    @GetMapping("/wallets/{walletId}")
    public ResponseEntity<WalletResponse> getWallet(
            @PathVariable @Size(max = 64) String walletId) {
        return walletQueryService.getWallet(walletId)
                .map(w -> ResponseEntity.ok(WalletResponse.from(w)))
                .orElse(ResponseEntity.notFound().build());
    }
}
