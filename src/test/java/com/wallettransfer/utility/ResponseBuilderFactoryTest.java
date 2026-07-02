package com.wallettransfer.utility;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.wallettransfer.builder.ResponseBuilderFactory;
import com.wallettransfer.constants.ApplicationConstants;
import com.wallettransfer.constants.ResponseConstant;
import org.junit.jupiter.api.Test;

class ResponseBuilderFactoryTest {

  @Test
  void shouldMarkSuccessCodesAsSuccess() {
    var response =
        ResponseBuilderFactory.getResponse(
            ApplicationConstants.SUCCESS, ResponseConstant.TRANSFER_SUCCESS);

    assertThat(response.getStatus()).isEqualTo(ApplicationConstants.SUCCESS);
    assertThat(response.getResponseCode()).isEqualTo(20001);
  }

  @Test
  void shouldMarkFailureCodesAsFail() {
    var response =
        ResponseBuilderFactory.getResponse("Insufficient funds", ResponseConstant.INSUFFICIENT_FUNDS);

    assertThat(response.getStatus()).isEqualTo(ApplicationConstants.FAIL);
    assertThat(response.getResponseCode()).isEqualTo(5002);
  }

  @Test
  void shouldRejectInvalidResponseCode() {
    assertThatThrownBy(() -> ResponseBuilderFactory.getResponse("bad", "not-a-number"))
        .isInstanceOf(IllegalArgumentException.class);
  }
}
