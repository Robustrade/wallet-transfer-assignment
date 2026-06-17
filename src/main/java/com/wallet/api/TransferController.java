package com.wallet.api;

import com.wallet.api.dto.CreateTransferRequest;
import com.wallet.api.dto.TransferDetailResponse;
import com.wallet.api.dto.TransferResponse;
import com.wallet.service.TransferExecutionResult;
import com.wallet.service.TransferService;
import jakarta.validation.Valid;
import java.util.UUID;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping
public class TransferController {

    private final TransferService transferService;

    public TransferController(TransferService transferService) {
        this.transferService = transferService;
    }

    @PostMapping("/transfers")
    public ResponseEntity<TransferResponse> createTransfer(
            @Valid @RequestBody CreateTransferRequest request) {
        TransferExecutionResult result = transferService.createTransfer(request);
        return ResponseEntity.status(result.httpStatus()).body(result.response());
    }

    @GetMapping("/transfers/{transferId}")
    public TransferDetailResponse getTransfer(@PathVariable UUID transferId) {
        return transferService.getTransfer(transferId);
    }
}
