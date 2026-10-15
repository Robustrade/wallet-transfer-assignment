package com.wallettransfer.utility;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallettransfer.dto.requestdto.CreateTransferRequestDTO;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;
import org.springframework.stereotype.Component;

@Component
public class RequestHashUtility {

  private final ObjectMapper objectMapper;

  public RequestHashUtility(ObjectMapper objectMapper) {
    this.objectMapper = objectMapper;
  }

  public String hashTransferRequest(CreateTransferRequestDTO request) {
    try {
      String canonical =
          request.getFromWalletId()
              + "|"
              + request.getToWalletId()
              + "|"
              + request.getAmount();
      MessageDigest digest = MessageDigest.getInstance("SHA-256");
      byte[] hash = digest.digest(canonical.getBytes(StandardCharsets.UTF_8));
      return HexFormat.of().formatHex(hash);
    } catch (NoSuchAlgorithmException ex) {
      throw new IllegalStateException("SHA-256 not available", ex);
    }
  }

  public String toJson(Object value) {
    try {
      return objectMapper.writeValueAsString(value);
    } catch (JsonProcessingException ex) {
      throw new IllegalStateException("Failed to serialize response payload", ex);
    }
  }

  public <T> T fromJson(String json, Class<T> type) {
    try {
      return objectMapper.readValue(json, type);
    } catch (JsonProcessingException ex) {
      throw new IllegalStateException("Failed to deserialize response payload", ex);
    }
  }
}
