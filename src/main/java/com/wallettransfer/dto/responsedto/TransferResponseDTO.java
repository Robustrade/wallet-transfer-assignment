package com.wallettransfer.dto.responsedto;

import com.wallettransfer.enums.TransferStatus;
import java.util.List;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Getter;
import lombok.NoArgsConstructor;
import lombok.Setter;
import lombok.ToString;

@Getter
@Setter
@AllArgsConstructor
@NoArgsConstructor
@Builder
@ToString
public class TransferResponseDTO {

  private String transferId;
  private String idempotencyKey;
  private String fromWalletId;
  private String toWalletId;
  private Long amount;
  private TransferStatus status;
  private String failureReason;
  private List<LedgerEntryResponseDTO> ledgerEntries;
}
