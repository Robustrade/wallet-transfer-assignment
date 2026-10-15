package com.wallettransfer.utility;

import com.wallettransfer.dto.responsedto.LedgerEntryResponseDTO;
import com.wallettransfer.dto.responsedto.TransferResponseDTO;
import com.wallettransfer.entity.LedgerEntry;
import com.wallettransfer.entity.Transfer;
import java.util.List;

public final class TransferMapper {

  private TransferMapper() {}

  public static TransferResponseDTO toTransferResponse(
      Transfer transfer, List<LedgerEntry> ledgerEntries) {
    List<LedgerEntryResponseDTO> entries =
        ledgerEntries.stream().map(TransferMapper::toLedgerEntryResponse).toList();

    return TransferResponseDTO.builder()
        .transferId(transfer.getId())
        .idempotencyKey(transfer.getIdempotencyKey())
        .fromWalletId(transfer.getFromWallet().getWalletId())
        .toWalletId(transfer.getToWallet().getWalletId())
        .amount(transfer.getAmount())
        .status(transfer.getStatus())
        .failureReason(transfer.getFailureReason())
        .ledgerEntries(entries)
        .build();
  }

  public static LedgerEntryResponseDTO toLedgerEntryResponse(LedgerEntry entry) {
    return LedgerEntryResponseDTO.builder()
        .entryId(entry.getId())
        .walletId(entry.getWallet().getWalletId())
        .transferId(entry.getTransfer().getId())
        .type(entry.getEntryType())
        .amount(entry.getAmount())
        .build();
  }
}
