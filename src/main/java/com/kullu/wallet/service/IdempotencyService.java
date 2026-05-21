package com.kullu.wallet.service;

import java.util.Optional;
import java.util.UUID;

import org.springframework.stereotype.Service;

import com.kullu.wallet.dto.response.IdempotencyResponse;
import com.kullu.wallet.entity.IdempotencyEntity;
import com.kullu.wallet.repository.IdempotencyRecordRepository;


@Service
public class IdempotencyService {

    private final IdempotencyRecordRepository idempotency;

    public IdempotencyService(final IdempotencyRecordRepository idempotency) {
        this.idempotency = idempotency;
    }

    public Optional<IdempotencyResponse> findByKey(final String key) {
        return idempotency.findByKey(key)
                .map(this::toCachedResponse);
    }

    /**
     * Atomically claim an idempotency key via native
     * {@code INSERT … ON CONFLICT DO NOTHING}.
     *
     * @return 1 if the key was claimed (row inserted), 0 if it already existed
     */
    public int tryClaim(final String key, final String requestHash, final UUID transferId) {
        return idempotency.tryClaim(key, requestHash, transferId);
    }

    public void storeResponse(final String key, final short status, final String body) {
        IdempotencyEntity idempotencyEntity = idempotency.findByKey(key)
                .orElseThrow(() -> new IllegalStateException(
                        "idempotency record should exist before storing response"));
        idempotencyEntity.storeResponse(status, body);
        idempotency.save(idempotencyEntity);
    }

    private IdempotencyResponse toCachedResponse(final IdempotencyEntity idempotencyEntity) {
        return new IdempotencyResponse(idempotencyEntity);
    }
}
