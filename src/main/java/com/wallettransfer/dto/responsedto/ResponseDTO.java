package com.wallettransfer.dto.responsedto;

import io.swagger.v3.oas.annotations.media.Schema;
import java.io.Serializable;
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
@Schema(
    description =
        """
        Standard API response envelope.

        - `status`: SUCCESS (responseCode 20000–29999) or FAIL otherwise
        - `responseCode`: business outcome code — see endpoint documentation for possible values
        - `data`: endpoint-specific payload (transfer details, balance, history list, etc.)
        """)
public class ResponseDTO implements Serializable {

  private static final long serialVersionUID = 1L;

  @Schema(description = "Outcome status", example = "SUCCESS", allowableValues = {"SUCCESS", "FAIL"})
  private String status;

  @Schema(description = "Human-readable message", example = "Transfer processed successfully")
  private String message;

  @Schema(description = "Business response code", example = "20001")
  private Integer responseCode;

  @Schema(description = "Endpoint-specific response payload")
  private transient Object data;
}
