package com.wallet.wallet.rest;

import com.wallet.wallet.dto.WalletRequestDTO;
import com.wallet.wallet.dto.WalletResponseDTO;
import com.wallet.wallet.service.WalletService;
import lombok.RequiredArgsConstructor;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

@RestController
@RequiredArgsConstructor
public class WalletController {

    private final WalletService walletService;

    @PostMapping("/wallet")
    public ResponseEntity<WalletResponseDTO> saveWallet(@RequestBody WalletRequestDTO walletRequestDTO){
        WalletResponseDTO walletResponseDTO = walletService.createWallet(walletRequestDTO);
        return new ResponseEntity<>(walletResponseDTO, HttpStatus.CREATED);
    }

    @GetMapping("/wallet/{walletId}")
    public ResponseEntity<Double> getAvailableBalance(@PathVariable int walletId){
        return new ResponseEntity<>(null);
    }

}
