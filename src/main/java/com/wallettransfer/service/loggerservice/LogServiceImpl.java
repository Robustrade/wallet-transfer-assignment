package com.wallettransfer.service.loggerservice;

import com.wallettransfer.utility.LogSanitizer;
import jakarta.servlet.http.HttpServletRequest;
import java.util.Objects;
import lombok.extern.slf4j.Slf4j;
import org.springframework.lang.Nullable;
import org.springframework.stereotype.Service;

@Slf4j
@Service
public class LogServiceImpl implements LogService {

  private static final String LOG_FORMAT = "[{}] {}";

  @Override
  public void printInfoLogs(
      @Nullable HttpServletRequest request, String className, String logMessage) {
    log.info(LOG_FORMAT, className, LogSanitizer.sanitize(logMessage));
  }

  @Override
  public void printResponseErrorLogs(
      @Nullable HttpServletRequest request, String className, String logMessage) {
    log.warn(LOG_FORMAT, className, LogSanitizer.sanitize(logMessage));
  }

  @Override
  public void printErrorLogs(
      @Nullable HttpServletRequest request, String className, Exception exception) {
    String message =
        Objects.requireNonNullElse(
            exception.getMessage(), exception.getClass().getSimpleName());
    log.error(LOG_FORMAT, className, LogSanitizer.sanitize(message), exception);
  }
}
