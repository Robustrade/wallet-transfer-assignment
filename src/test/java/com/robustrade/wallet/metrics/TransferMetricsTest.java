package com.robustrade.wallet.metrics;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.robustrade.wallet.service.TransferServiceException;
import io.micrometer.core.instrument.simple.SimpleMeterRegistry;
import java.math.BigDecimal;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

class TransferMetricsTest {

    private SimpleMeterRegistry registry;
    private TransferMetrics metrics;

    @BeforeEach
    void setUp() {
        registry = new SimpleMeterRegistry();
        metrics = new TransferMetrics(registry);
    }

    @Test
    void track_recordsThroughputLatencyAndSuccessOutcome() {
        String result = metrics.track(() -> {
            metrics.markOutcome(TransferMetricOutcome.NEW_PROCESSED, new BigDecimal("12.50"));
            return "ok";
        });

        assertThat(result).isEqualTo("ok");
        assertThat(registry.get(TransferMetrics.METRIC_REQUESTS).counter().count()).isEqualTo(1.0);
        assertThat(registry.get(TransferMetrics.METRIC_OUTCOMES)
                .tag("outcome", "NEW_PROCESSED")
                .counter()
                .count()).isEqualTo(1.0);
        assertThat(registry.get(TransferMetrics.METRIC_LATENCY)
                .tag("outcome", "NEW_PROCESSED")
                .timer()
                .count()).isEqualTo(1L);
        assertThat(registry.get(TransferMetrics.METRIC_AMOUNT).summary().count()).isEqualTo(1L);
        assertThat(registry.find(TransferMetrics.METRIC_ERRORS).counters()).isEmpty();
    }

    @Test
    void track_recordsErrorMetricsOnTransferServiceException() {
        assertThatThrownBy(() -> metrics.track(() -> {
            throw TransferServiceException.insufficientBalance("Insufficient balance");
        })).isInstanceOf(TransferServiceException.class);

        assertThat(registry.get(TransferMetrics.METRIC_REQUESTS).counter().count()).isEqualTo(1.0);
        assertThat(registry.get(TransferMetrics.METRIC_ERRORS)
                .tag("error_code", "INSUFFICIENT_BALANCE")
                .counter()
                .count()).isEqualTo(1.0);
        assertThat(registry.get(TransferMetrics.METRIC_INSUFFICIENT).counter().count()).isEqualTo(1.0);
        assertThat(registry.get(TransferMetrics.METRIC_LATENCY)
                .tag("outcome", "INSUFFICIENT_BALANCE")
                .timer()
                .count()).isEqualTo(1L);
    }

    @Test
    void markRetry_incrementsRetryCounter() {
        metrics.markRetry();
        assertThat(registry.get(TransferMetrics.METRIC_RETRIES).counter().count()).isEqualTo(1.0);
    }
}
