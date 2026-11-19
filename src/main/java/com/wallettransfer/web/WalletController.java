package com.wallettransfer.web;

import com.wallettransfer.service.WalletService;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.ResponseStatus;
import org.springframework.web.bind.annotation.RestController;

/** HTTP surface for creating wallets and reading their balance. */
@RestController
public class WalletController {

  private final WalletService wallets;

  public WalletController(WalletService wallets) {
    this.wallets = wallets;
  }

  @PostMapping("/wallets")
  @ResponseStatus(HttpStatus.CREATED)
  public WalletResponse create(@RequestBody CreateWalletRequest request) {
    return WalletResponse.from(wallets.create(request.id(), request.requireOpeningBalance()));
  }

  @GetMapping("/wallets/{id}")
  public WalletResponse get(@PathVariable String id) {
    return WalletResponse.from(wallets.get(id));
  }
}
