package com.wallettransfer.idempotency.service;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallettransfer.idempotency.domain.IdempotencyRecord;
import com.wallettransfer.idempotency.repository.IdempotencyRecordRepository;
import com.wallettransfer.transfer.dto.TransferResponse;
import java.util.Optional;
import java.util.UUID;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class IdempotencyService {

    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final ObjectMapper objectMapper;

    public IdempotencyService(
            IdempotencyRecordRepository idempotencyRecordRepository, ObjectMapper objectMapper) {
        this.idempotencyRecordRepository = idempotencyRecordRepository;
        this.objectMapper = objectMapper;
    }

    @Transactional(readOnly = true)
    public Optional<IdempotencyRecord> findByKey(String idempotencyKey) {
        return idempotencyRecordRepository.findById(idempotencyKey);
    }

    @Transactional
    public IdempotencyReservation reserve(String idempotencyKey, UUID transferId) {
        Optional<IdempotencyRecord> existing = idempotencyRecordRepository.findById(idempotencyKey);
        if (existing.isPresent()) {
            return new IdempotencyReservation(existing.get(), true);
        }

        IdempotencyRecord record = new IdempotencyRecord(idempotencyKey, transferId);
        try {
            idempotencyRecordRepository.saveAndFlush(record);
            return new IdempotencyReservation(record, false);
        } catch (DataIntegrityViolationException ex) {
            IdempotencyRecord raced = idempotencyRecordRepository
                    .findById(idempotencyKey)
                    .orElseThrow(() -> new IllegalStateException("Idempotency record disappeared"));
            return new IdempotencyReservation(raced, true);
        }
    }

    @Transactional
    public void storeResponse(IdempotencyRecord record, TransferResponse response, int httpStatus) {
        record.setResponse(writeResponse(response), httpStatus);
        idempotencyRecordRepository.save(record);
    }

    @Transactional(readOnly = true)
    public Optional<CachedTransferResult> getCachedResult(IdempotencyRecord record, boolean duplicate) {
        if (record.getResponseBody() == null) {
            return Optional.empty();
        }
        TransferResponse response = withDuplicateFlag(readResponse(record.getResponseBody()), duplicate);
        return Optional.of(new CachedTransferResult(response, record.getHttpStatus(), duplicate));
    }

    private String writeResponse(TransferResponse response) {
        try {
            return objectMapper.writeValueAsString(response);
        } catch (JsonProcessingException ex) {
            throw new IllegalStateException("Failed to serialize transfer response", ex);
        }
    }

    private TransferResponse readResponse(String json) {
        try {
            return objectMapper.readValue(json, TransferResponse.class);
        } catch (JsonProcessingException ex) {
            throw new IllegalStateException("Failed to deserialize transfer response", ex);
        }
    }

    private TransferResponse withDuplicateFlag(TransferResponse response, boolean duplicate) {
        return new TransferResponse(
                response.id(),
                response.idempotencyKey(),
                response.fromWalletId(),
                response.toWalletId(),
                response.amount(),
                response.status(),
                response.failureReason(),
                duplicate,
                response.createdAt());
    }

    public record IdempotencyReservation(IdempotencyRecord record, boolean duplicate) {
    }

    public record CachedTransferResult(TransferResponse response, int httpStatus, boolean duplicate) {
    }
}
