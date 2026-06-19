package com.wallet.transferservice.job;

import com.wallet.transferservice.repository.IdempotencyRecordRepository;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;
import org.springframework.transaction.annotation.Transactional;

import java.time.Instant;

@Component
public class IdempotencyCleanupJob {

    private static final Logger log = LoggerFactory.getLogger(IdempotencyCleanupJob.class);

    private final IdempotencyRecordRepository repository;

    public IdempotencyCleanupJob(IdempotencyRecordRepository repository) {
        this.repository = repository;
    }

    /**
     * Runs every hour to delete expired idempotency records.
     * Records expire 24 hours after creation.
     */
    @Scheduled(cron = "0 0 * * * *") // Runs at the top of every hour
    @Transactional
    public void cleanupExpiredRecords() {
        log.info("Starting idempotency records cleanup job...");
        int deletedCount = repository.deleteByExpiresAtBefore(Instant.now());
        log.info("Cleanup job completed. Deleted {} expired records.", deletedCount);
    }
}
