package com.rajat.wallet.controller;

import com.rajat.wallet.dto.PageResponse;
import com.rajat.wallet.dto.TransferResponse;
import com.rajat.wallet.dto.WalletResponse;
import com.rajat.wallet.service.WalletService;
import java.util.UUID;
import lombok.RequiredArgsConstructor;
import org.springframework.data.domain.Pageable;
import org.springframework.data.domain.Sort;
import org.springframework.data.web.PageableDefault;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/**
 * Read-only endpoints for wallet balance and transfer history. Thin transport layer: it delegates
 * to {@link WalletService} and lets {@code GlobalExceptionHandler} map a missing wallet to 404.
 */
@RestController
@RequestMapping("/wallets")
@RequiredArgsConstructor
public class WalletController {

  private final WalletService walletService;

  @GetMapping("/{id}")
  public WalletResponse getWallet(@PathVariable UUID id) {
    return walletService.getWallet(id);
  }

  @GetMapping("/{id}/transfers")
  public PageResponse<TransferResponse> getTransferHistory(
      @PathVariable UUID id,
      @PageableDefault(
              size = 20,
              sort = {"createdAt", "id"},
              direction = Sort.Direction.DESC)
          Pageable pageable) {
    return PageResponse.from(walletService.getTransferHistory(id, pageable));
  }
}
