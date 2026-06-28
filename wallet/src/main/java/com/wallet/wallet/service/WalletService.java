package com.wallet.wallet.service;

import com.wallet.wallet.dto.WalletRequestDTO;
import com.wallet.wallet.dto.WalletResponseDTO;
import com.wallet.wallet.mapper.WalletMapper;
import com.wallet.wallet.model.Wallet;
import com.wallet.wallet.repo.WalletRepo;
import lombok.RequiredArgsConstructor;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

import java.time.LocalDateTime;
import java.util.Date;

@Service
@RequiredArgsConstructor
public class WalletService {

    private final WalletMapper walletMapper;
    private final WalletRepo walletRepo;

    public WalletResponseDTO createWallet(WalletRequestDTO walletRequestDTO){
            Wallet wallet = walletMapper.toEntity(walletRequestDTO);
            wallet.setBlockAmount(0.0);
            LocalDateTime date = LocalDateTime.now();
            wallet.setCreatedAt(date);
            Wallet walletSaved = walletRepo.save(wallet);
            return new WalletResponseDTO(wallet.getName(), walletSaved.getWalletId(), wallet.getTotalBalance(), date);
    }


}
