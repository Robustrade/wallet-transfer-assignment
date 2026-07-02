package com.wallettransfer.dto.responsedto;

import com.wallettransfer.enums.LedgerEntryType;
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
public class LedgerEntryResponseDTO {

  private Long entryId;
  private String walletId;
  private String transferId;
  private LedgerEntryType type;
  private Long amount;
}
