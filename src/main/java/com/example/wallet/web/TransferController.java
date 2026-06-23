package com.example.wallet.web;

import com.example.wallet.domain.Transfer;
import com.example.wallet.domain.Wallet;
import com.example.wallet.repository.TransferRepository;
import com.example.wallet.repository.WalletRepository;
import com.example.wallet.service.TransferExecutionResult;
import com.example.wallet.service.TransferService;
import com.example.wallet.web.dto.TransferRequest;
import jakarta.validation.Valid;
import java.util.List;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.CrossOrigin;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/transfers")
@CrossOrigin(origins = "http://localhost:4200")
public class TransferController {

    private final TransferService transferService;
    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;

    public TransferController(
            TransferService transferService,
            WalletRepository walletRepository,
            TransferRepository transferRepository) {
        this.transferService = transferService;
        this.walletRepository = walletRepository;
        this.transferRepository = transferRepository;
    }

    @PostMapping
    public ResponseEntity<?> createTransfer(@Valid @RequestBody TransferRequest request) {
        TransferExecutionResult result = transferService.createTransfer(request);
        return ResponseEntity.status(result.statusCode()).body(result.body());
    }

    @GetMapping
    public List<Transfer> getAllTransfers() {
        return transferRepository.findAll();
    }

    @GetMapping("/wallets")
    public List<Wallet> getAllWallets() {
        return walletRepository.findAll();
    }
}
