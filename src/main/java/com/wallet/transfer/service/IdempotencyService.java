package com.wallet.transfer.service;

import com.wallet.transfer.domain.IdempotencyRecord;
import com.wallet.transfer.repository.IdempotencyRecordRepository;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Propagation;
import org.springframework.transaction.annotation.Transactional;

import java.time.LocalDateTime;
import java.util.Optional;

@Service
public class IdempotencyService {

    private final IdempotencyRecordRepository idempotencyRecordRepository;

    public IdempotencyService(IdempotencyRecordRepository idempotencyRecordRepository) {
        this.idempotencyRecordRepository = idempotencyRecordRepository;
    }

    /**
     * Atomically claims an idempotency key by inserting a pending row in its own committed
     * transaction (REQUIRES_NEW). Returns empty if the key was newly claimed, or the existing
     * record if another request already holds the key — enabling true in-progress detection.
     */
    @Transactional(propagation = Propagation.REQUIRES_NEW)
    public Optional<IdempotencyRecord> claimKey(String key, String hash) {
        try {
            idempotencyRecordRepository.saveAndFlush(
                    new IdempotencyRecord(key, hash, LocalDateTime.now())
            );
            return Optional.empty();
        } catch (DataIntegrityViolationException ex) {
            return idempotencyRecordRepository.findById(key);
        }
    }
}
