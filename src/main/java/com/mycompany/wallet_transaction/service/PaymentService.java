package com.mycompany.wallet_transaction.service;

import com.mycompany.wallet_transaction.dto.PaymentRequestDto;
import com.mycompany.wallet_transaction.dto.PaymentResponseDto;
import com.mycompany.wallet_transaction.entities.IdempotencyRecord;
import com.mycompany.wallet_transaction.entities.LedgerEntry;
import com.mycompany.wallet_transaction.entities.Transfer;
import com.mycompany.wallet_transaction.entities.Wallet;
import com.mycompany.wallet_transaction.entities.enums.IdempotencyStatus;
import com.mycompany.wallet_transaction.entities.enums.TransactionType;
import com.mycompany.wallet_transaction.entities.enums.TransferStatus;
import com.mycompany.wallet_transaction.repository.IdempotencyRecordRepository;
import com.mycompany.wallet_transaction.repository.LedgerEntryRepository;
import com.mycompany.wallet_transaction.repository.TransferRepository;
import com.mycompany.wallet_transaction.repository.WalletRepository;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.util.List;

@Service
@RequiredArgsConstructor
public class PaymentService {

    private final WalletRepository walletRepository;
    private final TransferRepository transferRepository;
    private final LedgerEntryRepository ledgerEntryRepository;
    private final IdempotencyRecordRepository idempotencyRecordRepository;


    @Transactional
    public PaymentResponseDto makeTransfer(PaymentRequestDto request) {
        int isKeyClaimed = idempotencyRecordRepository.claimIdempotencyKey(request.getIdempotencyKey());

        if (isKeyClaimed == 0) {
            return existingKeyResult(request.getIdempotencyKey());
        }

        IdempotencyRecord record = idempotencyRecordRepository.findByIdempotencyKey(request.getIdempotencyKey()).orElseThrow();

        List<Wallet> walletInTransaction = walletRepository.findAllByWalletIdInForUpdate(
                List.of(request.getFromWalletId(), request.getToWalletId())
        );

        if (walletInTransaction.size() != 2) {
            record.setStatus(IdempotencyStatus.FAILED);
            record.setResultStatus("FAILED");
            record.setResultMessage("Invalid Wallet");

            return PaymentResponseDto.builder()
                    .fromWalletId(request.getFromWalletId())
                    .toWalletId(request.getToWalletId())
                    .amount(request.getAmount())
                    .status(TransferStatus.FAILED)
                    .message("Invalid wallet")
                    .build();
        }

        Wallet sourceWallet = walletInTransaction.stream()
                .filter(wallet -> wallet.getWalletId().equals(request.getFromWalletId()))
                .findFirst()
                .orElseThrow();

        Wallet destinationWallet = walletInTransaction.stream()
                .filter(wallet -> wallet.getWalletId().equals(request.getToWalletId()))
                .findFirst()
                .orElseThrow();

        Transfer transfer = Transfer.builder()
                .sourceWallet(sourceWallet)
                .destinationWallet(destinationWallet)
                .amount(request.getAmount())
                .status(TransferStatus.PENDING)
                .build();

        transferRepository.save(transfer);

        if (sourceWallet.getBalance().compareTo(request.getAmount()) < 0) {
            transfer.setStatus(TransferStatus.FAILED);

            record.setStatus(IdempotencyStatus.COMPLETED);
            record.setTransfer(transfer);
            record.setResultStatus("FAILED");
            record.setResultMessage("Insufficient balance");

            return PaymentResponseDto.builder()
                    .transferId(transfer.getTransferId())
                    .fromWalletId(sourceWallet.getWalletId())
                    .toWalletId(destinationWallet.getWalletId())
                    .amount(request.getAmount())
                    .status(TransferStatus.FAILED)
                    .message("Insufficient balance")
                    .build();
        }

        sourceWallet.setBalance(sourceWallet.getBalance().subtract(request.getAmount()));
        destinationWallet.setBalance(destinationWallet.getBalance().add(request.getAmount()));

        LedgerEntry debitEntry = LedgerEntry.builder()
                .transfer(transfer)
                .wallet(sourceWallet)
                .amount(request.getAmount())
                .transactionType(TransactionType.DEBIT)
                .build();

        LedgerEntry creditEntry = LedgerEntry.builder()
                .transfer(transfer)
                .wallet(destinationWallet)
                .amount(request.getAmount())
                .transactionType(TransactionType.CREDIT)
                .build();

        ledgerEntryRepository.save(debitEntry);
        ledgerEntryRepository.save(creditEntry);

        record.setStatus(IdempotencyStatus.COMPLETED);
        record.setTransfer(transfer);
        record.setResultStatus("PROCESSED");
        record.setResultMessage("Transfer processed successfully");

        return PaymentResponseDto.builder()
                .transferId(transfer.getTransferId())
                .fromWalletId(sourceWallet.getWalletId())
                .toWalletId(destinationWallet.getWalletId())
                .amount(request.getAmount())
                .status(TransferStatus.PROCESSED)
                .message("Transfer processed successfully")
                .build();
    }
    private PaymentResponseDto existingKeyResult(String idempotencyKey) {
        IdempotencyRecord record = idempotencyRecordRepository
                .findByIdempotencyKey(idempotencyKey)
                .orElseThrow();

        if (record.getStatus() == IdempotencyStatus.IN_PROGRESS) {
            return PaymentResponseDto.builder()
                    .status(TransferStatus.PENDING)
                    .message("Request is still processing")
                    .build();
        }

        Transfer transfer = record.getTransfer();

        return PaymentResponseDto.builder()
                .transferId(transfer != null ? transfer.getTransferId() : null)
                .fromWalletId(transfer != null ? transfer.getSourceWallet().getWalletId() : null)
                .toWalletId(transfer != null ? transfer.getDestinationWallet().getWalletId() : null)
                .amount(transfer != null ? transfer.getAmount() : null)
                .status(TransferStatus.valueOf(record.getResultStatus()))
                .message(record.getResultMessage())
                .build();
    }
}
