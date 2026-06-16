package com.viditec.wallettransfer.service;

import com.viditec.wallettransfer.dto.CreateWalletDto;
import com.viditec.wallettransfer.dto.WalletDto;
import com.viditec.wallettransfer.exception.InvalidTransferRequestException;
import com.viditec.wallettransfer.exception.ResourceNotFoundException;
import com.viditec.wallettransfer.repository.WalletOperations;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.stereotype.Service;

@Service
public class WalletService {

    private final WalletOperations walletRepository;

    public WalletService(WalletOperations walletRepository) {
        this.walletRepository = walletRepository;
    }

    public WalletDto createWallet(CreateWalletDto request) {
        try {
            walletRepository.createWallet(request.walletId(), request.initialBalance());
            return new WalletDto(request.walletId(), request.initialBalance());
        } catch (DuplicateKeyException exception) {
            throw new InvalidTransferRequestException(
                    "Wallet '%s' already exists.".formatted(request.walletId())
            );
        }
    }

    public WalletDto getWallet(String walletId) {
        return walletRepository.findById(walletId)
                .orElseThrow(() -> new ResourceNotFoundException("Wallet", walletId));
    }
}
