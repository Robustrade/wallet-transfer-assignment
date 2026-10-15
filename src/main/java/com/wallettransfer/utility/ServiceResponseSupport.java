package com.wallettransfer.utility;

import com.wallettransfer.builder.ResponseBuilderFactory;
import com.wallettransfer.constants.ApplicationConstants;
import com.wallettransfer.constants.ResponseConstant;
import com.wallettransfer.dto.responsedto.ResponseDTO;
import com.wallettransfer.exception.ResponseException;
import com.wallettransfer.service.loggerservice.LogService;
import jakarta.servlet.http.HttpServletRequest;
import java.util.function.Supplier;
import org.springframework.stereotype.Component;

@Component
public class ServiceResponseSupport {

  private final LogService logService;
  private final HttpServletRequest request;

  public ServiceResponseSupport(LogService logService, HttpServletRequest request) {
    this.logService = logService;
    this.request = request;
  }

  public ResponseDTO execute(String className, Supplier<ResponseDTO> action) {
    try {
      return action.get();
    } catch (ResponseException responseException) {
      logService.printResponseErrorLogs(
          request, className, LogSanitizer.sanitize(responseException.getMessage()));
      return ResponseBuilderFactory.getResponse(
          responseException.getMessage(),
          responseException.getResponseCode(),
          responseException.getData());
    } catch (RuntimeException runtimeException) {
      logService.printErrorLogs(request, className, runtimeException);
      return ResponseBuilderFactory.getResponse(
          ApplicationConstants.FAIL, ResponseConstant.FAILURE_CODE);
    }
  }
}
