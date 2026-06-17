package com.wallettransfer.transfer.web;

import com.wallettransfer.ledger.dto.LedgerEntryResponse;
import com.wallettransfer.ledger.service.LedgerService;
import com.wallettransfer.transfer.dto.CreateTransferRequest;
import com.wallettransfer.transfer.dto.TransferResponse;
import com.wallettransfer.transfer.service.TransferService;
import com.wallettransfer.transfer.service.TransferService.TransferResult;
import jakarta.validation.Valid;
import java.util.List;
import java.util.UUID;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api/transfers")
public class TransferController {

    private final TransferService transferService;
    private final LedgerService ledgerService;

    public TransferController(TransferService transferService, LedgerService ledgerService) {
        this.transferService = transferService;
        this.ledgerService = ledgerService;
    }

    @PostMapping
    public ResponseEntity<TransferResponse> createTransfer(@Valid @RequestBody CreateTransferRequest request) {
        TransferResult result = transferService.createTransfer(request);
        return ResponseEntity.status(result.httpStatus()).body(result.response());
    }

    @GetMapping
    public List<TransferResponse> listTransfers() {
        return transferService.listTransfers();
    }

    @GetMapping("/{transferId}/ledger")
    public List<LedgerEntryResponse> listLedger(@PathVariable UUID transferId) {
        return ledgerService.listByTransferId(transferId);
    }
}
