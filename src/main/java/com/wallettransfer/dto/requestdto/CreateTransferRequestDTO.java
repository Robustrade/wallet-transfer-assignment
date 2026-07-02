package com.wallettransfer.dto.requestdto;

import io.swagger.v3.oas.annotations.media.Schema;
import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
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
@Schema(description = "Request to create an idempotent wallet-to-wallet transfer")
public class CreateTransferRequestDTO {

  @NotBlank(message = "idempotencyKey is required")
  @Schema(
      description = "Client-supplied unique key for exactly-once API semantics",
      example = "abc123")
  private String idempotencyKey;

  @NotBlank(message = "fromWalletId is required")
  @Schema(description = "Source wallet identifier", example = "wallet_1")
  private String fromWalletId;

  @NotBlank(message = "toWalletId is required")
  @Schema(description = "Destination wallet identifier", example = "wallet_2")
  private String toWalletId;

  @NotNull(message = "amount is required")
  @Min(value = 1, message = "amount must be greater than zero")
  @Schema(description = "Transfer amount in smallest currency unit", example = "100", minimum = "1")
  private Long amount;
}
