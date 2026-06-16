package com.wallet.controller;

import com.wallet.controller.dto.TransferResponse;
import com.wallet.controller.dto.WalletBalanceResponse;
import com.wallet.domain.Wallet;
import com.wallet.exception.UnknownClientException;
import com.wallet.repository.ClientRepository;
import com.wallet.service.WalletService;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import java.util.List;

/**
 * Thin REST handler for wallet queries; delegates to {@link WalletService}.
 *
 * <p>Both read endpoints require an {@code X-Client-Id} header that resolves
 * to a registered client. The {@link com.wallet.filter.SignatureFilter
 * SignatureFilter} only fires for state-mutating verbs against
 * {@code /transfers}, so without this check the read endpoints would be open
 * to any caller — an information-disclosure issue in a multi-tenant context.
 *
 * <p>Wallet-belongs-to-client is <em>not</em> enforced here because the data
 * model has no ownership column today. If wallet ownership is introduced, this
 * is the right place to add the second-level check.
 */
@RestController
@RequestMapping("/wallets")
public class WalletController {

    private final WalletService walletService;
    private final ClientRepository clientRepository;

    public WalletController(WalletService walletService, ClientRepository clientRepository) {
        this.walletService = walletService;
        this.clientRepository = clientRepository;
    }

    @GetMapping("/{walletId}/balance")
    public ResponseEntity<WalletBalanceResponse> getBalance(
            @RequestHeader("X-Client-Id") String clientId,
            @PathVariable String walletId) {
        requireKnownClient(clientId);
        Wallet wallet = walletService.getWallet(walletId);
        return ResponseEntity.ok(new WalletBalanceResponse(
                wallet.getId(),
                wallet.getBalance(),
                wallet.getUpdatedAt()
        ));
    }

    @GetMapping("/{walletId}/transfers")
    public ResponseEntity<List<TransferResponse>> getTransferHistory(
            @RequestHeader("X-Client-Id") String clientId,
            @PathVariable String walletId,
            @RequestParam(name = "limit", defaultValue = "" + WalletService.DEFAULT_HISTORY_LIMIT) int limit) {
        requireKnownClient(clientId);
        List<TransferResponse> responses = walletService.getTransferHistory(walletId, limit).stream()
                .map(TransferResponse::from)
                .toList();
        return ResponseEntity.ok(responses);
    }

    /**
     * Mirrors {@link com.wallet.service.TransferService#executeTransfer}'s
     * up-front existsById check so unknown callers get a clean 403 (via the
     * global handler's {@link UnknownClientException} mapping) instead of
     * leaking wallet existence through 200 vs 404 timing.
     */
    private void requireKnownClient(String clientId) {
        if (!clientRepository.existsById(clientId)) {
            throw new UnknownClientException(clientId);
        }
    }
}
