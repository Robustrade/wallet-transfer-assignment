package com.robustrade.wallet.handler;

import com.robustrade.wallet.handler.dto.CreateTransferRequest;
import com.robustrade.wallet.handler.dto.TransferResponse;
import com.robustrade.wallet.service.TransferService;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@Slf4j
@RestController
@RequestMapping("/transfers")
@RequiredArgsConstructor
public class TransferHandler {

    private final TransferService transferService;

    @PostMapping
    public ResponseEntity<TransferResponse> createTransfer(
            @Valid @RequestBody CreateTransferRequest request
    ) {
        log.info(
                "Received create transfer request: idempotencyKey={}, fromWalletId={}, toWalletId={}",
                request.getIdempotencyKey(),
                request.getFromWalletId(),
                request.getToWalletId()
        );

        try {
            TransferResponse response = transferService.createTransfer(request);
            log.info(
                    "Create transfer completed: transferId={}, state={}, idempotencyKey={}",
                    response.getTransferId(),
                    response.getState(),
                    response.getIdempotencyKey()
            );
            log.debug("Create transfer response: {}", response);
            return ResponseEntity.status(HttpStatus.CREATED).body(response);
        } catch (Exception ex) {
            log.error(
                    "Create transfer failed for idempotencyKey={}: {}",
                    request.getIdempotencyKey(),
                    ex.getMessage(),
                    ex
            );
            throw ex;
        }
    }
}
