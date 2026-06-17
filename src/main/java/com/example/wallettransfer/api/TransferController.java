package com.example.wallettransfer.api;

import com.example.wallettransfer.application.CreateTransferCommand;
import com.example.wallettransfer.application.TransferResult;
import com.example.wallettransfer.application.TransferService;
import com.example.wallettransfer.domain.Transfer;
import com.example.wallettransfer.domain.Wallet;
import com.example.wallettransfer.persistence.TransferRepository;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

import java.util.UUID;

@RestController
public class TransferController {

    private final TransferService transferService;
    private final TransferRepository transferRepository;

    public TransferController(TransferService transferService, TransferRepository transferRepository) {
        this.transferService = transferService;
        this.transferRepository = transferRepository;
    }

    /**
     * POST /transfers
     *
     * Creates a new wallet-to-wallet transfer. Idempotent: repeating the request
     * with the same idempotencyKey returns the original result without re-executing
     * the transfer.
     */
    @PostMapping("/transfers")
    public ResponseEntity<TransferResponse> createTransfer(@RequestBody TransferRequest request) {
        CreateTransferCommand command = new CreateTransferCommand(
                request.idempotencyKey(),
                request.fromWalletId(),
                request.toWalletId(),
                request.amount());

        TransferResult result = transferService.createTransfer(command);
        return ResponseEntity.status(HttpStatus.CREATED).body(TransferResponse.from(result));
    }

    /**
     * GET /transfers/{id}
     *
     * Returns the current state and details of a transfer by its UUID.
     */
    @GetMapping("/transfers/{id}")
    public ResponseEntity<TransferResponse> getTransfer(@PathVariable UUID id) {
        return transferRepository.findById(id)
                .map(transfer -> ResponseEntity.ok(toTransferResponse(transfer)))
                .orElse(ResponseEntity.notFound().build());
    }

    /**
     * GET /wallets/{id}
     *
     * Returns the current balance of a wallet.
     */
    @GetMapping("/wallets/{id}")
    public ResponseEntity<WalletResponse> getWallet(@PathVariable String id) {
        return transferRepository.findWalletById(id)
                .map(wallet -> ResponseEntity.ok(new WalletResponse(wallet.id(), wallet.balance())))
                .orElse(ResponseEntity.notFound().build());
    }

    private TransferResponse toTransferResponse(Transfer transfer) {
        return new TransferResponse(
                transfer.id(),
                transfer.state(),
                transfer.fromWalletId(),
                transfer.toWalletId(),
                transfer.amount(),
                transfer.failureReason());
    }
}
