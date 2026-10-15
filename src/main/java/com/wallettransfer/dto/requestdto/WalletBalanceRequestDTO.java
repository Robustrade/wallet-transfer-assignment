package com.wallettransfer.dto.requestdto;

import io.swagger.v3.oas.annotations.media.Schema;
import jakarta.validation.constraints.NotBlank;
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
@Schema(description = "Request to fetch the current balance of a wallet")
public class WalletBalanceRequestDTO {

  @NotBlank(message = "walletId is required")
  @Schema(description = "Wallet identifier", example = "wallet_1")
  private String walletId;
}
