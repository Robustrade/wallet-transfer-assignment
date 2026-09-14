package com.robustrade.wallet.handler;

import com.robustrade.wallet.handler.dto.CreateWalletRequest;
import com.robustrade.wallet.handler.dto.WalletResponse;
import com.robustrade.wallet.service.WalletService;
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
@RequestMapping("/wallets")
@RequiredArgsConstructor
public class WalletHandler {

    private final WalletService walletService;

    @PostMapping
    public ResponseEntity<WalletResponse> createWallet(@Valid @RequestBody CreateWalletRequest request) {
        log.info("Received create wallet request for userId={}", request.getUserId());
        log.debug(
                "Create wallet request details: userId={}, balance={}",
                request.getUserId(),
                request.getBalance()
        );

        try {
            WalletResponse response = walletService.createWallet(request);
            log.info(
                    "Create wallet completed successfully: walletId={}, userId={}",
                    response.getWalletId(),
                    response.getUserId()
            );
            log.debug("Create wallet response: {}", response);
            return ResponseEntity.status(HttpStatus.CREATED).body(response);
        } catch (Exception ex) {
            log.error(
                    "Create wallet failed for userId={}: {}",
                    request.getUserId(),
                    ex.getMessage(),
                    ex
            );
            throw ex;
        }
    }
}
