package com.robustrade.wallet.handler;

import com.robustrade.wallet.handler.dto.StatementResponse;
import com.robustrade.wallet.service.StatementService;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@Slf4j
@RestController
@RequestMapping("/statements")
@RequiredArgsConstructor
public class StatementHandler {

    private final StatementService statementService;

    @GetMapping
    public ResponseEntity<StatementResponse> getStatement(
            @RequestParam(required = false) Long userId,
            @RequestParam(required = false) String walletId
    ) {
        log.info("Received statement request for userId={}, walletId={}", userId, walletId);

        try {
            StatementResponse response = statementService.getStatement(userId, walletId);
            log.info(
                    "Statement fetched successfully: walletId={}, userId={}, entryCount={}",
                    response.getWalletId(),
                    response.getUserId(),
                    response.getEntries() != null ? response.getEntries().size() : 0
            );
            log.debug("Statement response: {}", response);
            return ResponseEntity.ok(response);
        } catch (Exception ex) {
            log.error(
                    "Statement request failed for userId={}, walletId={}: {}",
                    userId,
                    walletId,
                    ex.getMessage(),
                    ex
            );
            throw ex;
        }
    }
}
