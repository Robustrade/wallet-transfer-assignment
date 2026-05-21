package com.kullu.wallet.controller;

import jakarta.validation.Valid;

import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import com.kullu.wallet.dto.request.CreateTransferRequest;
import com.kullu.wallet.dto.response.TransferOutcome;
import com.kullu.wallet.service.TransferService;

@RestController
@RequestMapping("/transfers")
public class TransferController {

    private final TransferService transferService;

    public TransferController(final TransferService transferService) {
        this.transferService = transferService;
    }

    /**
     * The body is intentionally returned as a raw {@link String} rather than a
     * typed DTO: on idempotent replay we return the exact bytes that were
     * cached on the first execution. Re-serialising through a DTO here would
     * risk Jackson reordering keys and breaking byte-for-byte equality between
     * the original response and replays.
     *
     * <p>Because the return type is {@code String}, Spring's
     * {@code StringHttpMessageConverter} would otherwise default the
     * {@code Content-Type} response header to {@code text/plain}. We
     * therefore declare {@code produces = application/json} on the mapping
     * and set the {@code Content-Type} explicitly on every response so
     * content-negotiating clients see a correctly-typed JSON body.
     */
    @PostMapping(produces = MediaType.APPLICATION_JSON_VALUE)
    public ResponseEntity<String> create(@Valid @RequestBody final CreateTransferRequest request) {
        TransferOutcome outcome = transferService.createTransfer(request);
        int responseStatus = outcome.isReplayed() ? 200 : outcome.getStatus();
        return ResponseEntity.status(responseStatus)
            .contentType(MediaType.APPLICATION_JSON)
            .body(outcome.getResponseBody());
    }
}
