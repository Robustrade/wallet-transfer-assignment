package com.wallettransfer.service.loggerservice;

import jakarta.servlet.http.HttpServletRequest;
import org.springframework.lang.Nullable;
import org.springframework.stereotype.Service;

@Service
public interface LogService {

  void printInfoLogs(@Nullable HttpServletRequest request, String className, String logMessage);

  void printResponseErrorLogs(
      @Nullable HttpServletRequest request, String className, String logMessage);

  void printErrorLogs(@Nullable HttpServletRequest request, String className, Exception exception);
}
