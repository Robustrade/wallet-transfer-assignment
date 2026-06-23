package com.example.wallet.service;

import com.example.wallet.domain.IdempotencyRecord;
import com.example.wallet.repository.IdempotencyRecordRepository;
import com.example.wallet.web.dto.TransferResponse;
import tools.jackson.core.JacksonException;
import tools.jackson.databind.ObjectMapper;
import java.util.Optional;
import org.springframework.stereotype.Service;

@Service
public class IdempotencyService {

    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final ObjectMapper objectMapper;

    public IdempotencyService(
            IdempotencyRecordRepository idempotencyRecordRepository, ObjectMapper objectMapper) {
        this.idempotencyRecordRepository = idempotencyRecordRepository;
        this.objectMapper = objectMapper;
    }

    public Optional<TransferExecutionResult> findCachedResult(String idempotencyKey) {
        return idempotencyRecordRepository
                .findById(idempotencyKey)
                .map(this::toExecutionResult);
    }

    public void storeResult(String idempotencyKey, TransferExecutionResult result) {
        idempotencyRecordRepository.save(new IdempotencyRecord(
                idempotencyKey, result.statusCode(), serialize(result.body())));
    }

    private TransferExecutionResult toExecutionResult(IdempotencyRecord record) {
        return new TransferExecutionResult(record.getStatusCode(), deserialize(record.getResponseBody()));
    }

    private String serialize(TransferResponse response) {
        try {
            return objectMapper.writeValueAsString(response);
        } catch (JacksonException exception) {
            throw new IllegalStateException("Failed to serialize transfer response", exception);
        }
    }

    private TransferResponse deserialize(String responseBody) {
        try {
            return objectMapper.readValue(responseBody, TransferResponse.class);
        } catch (JacksonException exception) {
            throw new IllegalStateException("Failed to deserialize transfer response", exception);
        }
    }
}
