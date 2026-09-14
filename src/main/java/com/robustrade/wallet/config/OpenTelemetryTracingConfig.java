package com.robustrade.wallet.config;

import io.opentelemetry.exporter.logging.LoggingSpanExporter;
import io.opentelemetry.sdk.trace.export.SimpleSpanProcessor;
import org.springframework.boot.actuate.autoconfigure.tracing.SdkTracerProviderBuilderCustomizer;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

/**
 * Exports OpenTelemetry spans to application logs (console) for local debugging.
 * Sampling is controlled by {@code management.tracing.sampling.probability}.
 */
@Configuration
public class OpenTelemetryTracingConfig {

    @Bean
    SdkTracerProviderBuilderCustomizer loggingSpanProcessorCustomizer() {
        LoggingSpanExporter exporter = LoggingSpanExporter.create();
        return builder -> builder.addSpanProcessor(SimpleSpanProcessor.create(exporter));
    }
}
