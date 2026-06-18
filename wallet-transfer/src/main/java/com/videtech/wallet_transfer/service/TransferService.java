package com.videtech.wallet_transfer.service;

import java.math.BigDecimal;
import java.time.LocalDateTime;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import com.videtech.wallet_transfer.domain.LedgerEntry;
import com.videtech.wallet_transfer.domain.LedgerType;
import com.videtech.wallet_transfer.domain.Transfer;
import com.videtech.wallet_transfer.domain.TransferStatus;
import com.videtech.wallet_transfer.domain.Wallet;
import com.videtech.wallet_transfer.exception.InsufficientBalanceExceptions;
import com.videtech.wallet_transfer.repository.LedgerEntryRepository;
import com.videtech.wallet_transfer.repository.TransferRepository;
import com.videtech.wallet_transfer.repository.WalletRepository;


import lombok.RequiredArgsConstructor;

@Service
@RequiredArgsConstructor
public class TransferService {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;

    @Transactional(noRollbackFor = InsufficientBalanceExceptions.class)
    public Transfer execute(String idempotencyKey, String fromWalletId, String toWalletId, BigDecimal amount) {
       
    	Optional<Transfer> existing = transferRepository.findByIdempotencyKey(idempotencyKey);
     	if (existing.isPresent()) {
     	    Transfer prior = existing.get();
     	    if (!prior.getFromWalletId().equals(fromWalletId)
     	            || !prior.getToWalletId().equals(toWalletId)
     	            || prior.getAmount().compareTo(amount) != 0) {
     	        throw new RuntimeException("Idempotency key reuse with different parameters: " + idempotencyKey);
     	    }
     	    if (prior.getStatus() == TransferStatus.FAILED) {
     	        throw new RuntimeException(prior.getResponseBody() != null ? prior.getResponseBody() : "Transfer previously failed");
     	    }
     	    return prior;
     	}
       
    	Transfer transfer = new Transfer();
    	transfer.setId(UUID.randomUUID().toString());
    	transfer.setIdempotencyKey(idempotencyKey);
    	transfer.setFromWalletId(fromWalletId);
    	transfer.setToWalletId(toWalletId);
    	transfer.setAmount(amount);
    	transfer.setStatus(TransferStatus.PENDING);
    	transfer.setCreatedAt(LocalDateTime.now());
    	transfer.setUpdatedAt(LocalDateTime.now());
    	
    	try {
    	    transferRepository.saveAndFlush(transfer);
    	} catch (org.springframework.dao.DataIntegrityViolationException e) {
    	    return transferRepository.findByIdempotencyKey(idempotencyKey)
    	            .orElseThrow(() -> new RuntimeException("Idempotency conflict"));
    	}

        try {
            if (fromWalletId.equals(toWalletId)) {
                throw new RuntimeException("fromWalletId and toWalletId must be different");
            }
            List<String> ordered = List.of(fromWalletId, toWalletId)
                    .stream()
                    .sorted()
                    .toList();

            Wallet first = walletRepository.findByIdWithLock(ordered.get(0))
                    .orElseThrow(() -> new RuntimeException("Wallet not found: " + ordered.get(0)));

            Wallet second = walletRepository.findByIdWithLock(ordered.get(1))
                    .orElseThrow(() -> new RuntimeException("Wallet not found: " + ordered.get(1)));

            
            Wallet fromWallet = first.getId().equals(fromWalletId) ? first : second;
            Wallet toWallet = first.getId().equals(toWalletId) ? first : second;

           
            if (fromWallet.getBalance().compareTo(amount) < 0) {
                transfer.setStatus(TransferStatus.FAILED);
                transfer.setUpdatedAt(LocalDateTime.now());
                transferRepository.saveAndFlush(transfer);
                throw new InsufficientBalanceExceptions("Insufficient balance in wallet: " + fromWalletId);
            }

            
            fromWallet.setBalance(fromWallet.getBalance().subtract(amount));
            toWallet.setBalance(toWallet.getBalance().add(amount));
            walletRepository.save(fromWallet);
            walletRepository.save(toWallet);

            
            LedgerEntry debit = new LedgerEntry();
            debit.setId(UUID.randomUUID().toString());
            debit.setWalletId(fromWalletId);
            debit.setTransferId(transfer.getId());
            debit.setType(LedgerType.DEBIT);
            debit.setAmount(amount);
            debit.setCreatedAt(LocalDateTime.now());

            LedgerEntry credit = new LedgerEntry();
            credit.setId(UUID.randomUUID().toString());
            credit.setWalletId(toWalletId);
            credit.setTransferId(transfer.getId());
            credit.setType(LedgerType.CREDIT);
            credit.setAmount(amount);
            credit.setCreatedAt(LocalDateTime.now());

            ledgerEntryRepository.save(debit);
            ledgerEntryRepository.save(credit);

           

            transfer.setStatus(TransferStatus.PROCESSED);
            transferRepository.save(transfer);
           
            return transfer;

         
    } catch (InsufficientBalanceExceptions e) {
        throw e;
    } catch (RuntimeException e) {
        transfer.setStatus(TransferStatus.FAILED);
        transfer.setUpdatedAt(LocalDateTime.now());
        transferRepository.saveAndFlush(transfer);
        throw e;
    }
    
    }
    

}
