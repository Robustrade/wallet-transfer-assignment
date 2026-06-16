package com.wallet.controller;

import com.wallet.controller.dto.CreateTransferRequest;
import com.wallet.service.TransferService;
import com.wallet.util.LogSafe;
import jakarta.validation.Valid;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import java.nio.charset.StandardCharsets;

/** Thin REST handler — validates the request and delegates to {@link TransferService}. */
@RestController
@RequestMapping("/transfers")
public class TransferController {

    private static final Logger log = LoggerFactory.getLogger(TransferController.class);

    private final TransferService transferService;

    public TransferController(TransferService transferService) {
        this.transferService = transferService;
    }

    /**
     * Create a wallet-to-wallet transfer.
     *
     * <p>Status codes: 201 first execution, 200 idempotent replay, 400 validation,
     * 403 unknown client, 404 wallet missing, 409 key reused with different payload,
     * 422 insufficient balance.
     *
     * <p>X-Client-Id is required — idempotency keys are scoped per (clientId, key),
     * so a missing header would collapse tenants into a shared scope.
     *
     * <p>Both field-level constraints (e.g. {@code @NotBlank}, {@code @Positive})
     * and the cross-field same-wallet rule are JSR-303 annotations on
     * {@link CreateTransferRequest}; {@code @Valid} triggers them in one pass
     * and any failure surfaces as
     * {@link org.springframework.web.bind.MethodArgumentNotValidException}.
     */
    @PostMapping
    public ResponseEntity<Object> createTransfer(
            @RequestHeader("X-Client-Id") String clientId,
            @RequestHeader("Idempotency-Key") String idempotencyKey,
            @Valid @RequestBody CreateTransferRequest request) {
        // Guard the debug line: SLF4J's {} placeholder defers formatting, but
        // arguments — including LogSafe.mask — are still evaluated eagerly,
        // which is wasted work on every request when com.wallet runs at INFO.
        if (log.isDebugEnabled()) {
            log.debug("Received transfer request: clientId={}, idempotencyKey={}",
                    clientId, LogSafe.mask(idempotencyKey));
        }

        TransferService.TransferResult result = transferService.executeTransfer(clientId, idempotencyKey, request);

        // Pattern-switch on the sealed hierarchy: the compiler enforces
        // exhaustive coverage, so a future TransferResult variant fails the
        // build here (where it can be fixed) instead of at request time.
        //
        // Replay path ships the cached body as raw bytes — going through
        // Jackson would parse the cached String, build a Map, then serialise
        // it back to a near-identical String on every duplicate request.
        // byte[] body + explicit application/json Content-Type lets the
        // ByteArrayHttpMessageConverter write the payload verbatim.
        return switch (result) {
            case TransferService.TransferResult.Fresh fresh -> ResponseEntity
                    .status(fresh.httpStatus())
                    .body(fresh.response());
            case TransferService.TransferResult.Replay replay -> ResponseEntity
                    .status(replay.httpStatus())
                    .contentType(MediaType.APPLICATION_JSON)
                    .body(replay.rawJson().getBytes(StandardCharsets.UTF_8));
        };
    }
}
