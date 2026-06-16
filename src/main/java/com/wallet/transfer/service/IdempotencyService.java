package com.wallet.transfer.service;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.transfer.dto.TransferRequest;
import com.wallet.transfer.dto.TransferResponse;
import com.wallet.transfer.entity.IdempotencyRecord;
import com.wallet.transfer.repository.IdempotencyRecordRepository;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Propagation;
import org.springframework.transaction.annotation.Transactional;

import java.util.Optional;
import com.wallet.transfer.exception.ConflictException;

@Service
@RequiredArgsConstructor
public class IdempotencyService {

    private final IdempotencyRecordRepository idempotencyRecordRepository;
    private final ObjectMapper objectMapper;

    @Transactional(propagation = Propagation.REQUIRES_NEW)
    public IdempotencyRecord checkAndInitializeIdempotencyRecord(TransferRequest request) {
        Optional<IdempotencyRecord> existingRecord = idempotencyRecordRepository.findById(request.getIdempotencyKey());

        if (existingRecord.isPresent()) {
            IdempotencyRecord record = existingRecord.get();
            if (record.getResponseStatus() == null) {
                throw new ConflictException("A request with this idempotency key is currently being processed.");
            }
            return record;
        }

        try {
            IdempotencyRecord newRecord = IdempotencyRecord.builder()
                    .idempotencyKey(request.getIdempotencyKey())
                    .requestPayload(objectMapper.writeValueAsString(request))
                    .build();
            return idempotencyRecordRepository.save(newRecord);
        } catch (JsonProcessingException e) {
            throw new RuntimeException("Failed to serialize request payload", e);
        }
    }

    @Transactional(propagation = Propagation.REQUIRES_NEW)
    public void updateIdempotencyRecord(String idempotencyKey, TransferResponse response, int status) {
        IdempotencyRecord record = idempotencyRecordRepository.findById(idempotencyKey)
                .orElseThrow(() -> new IllegalStateException("Idempotency record not found"));
        try {
            record.setResponseBody(objectMapper.writeValueAsString(response));
            record.setResponseStatus(status);
            idempotencyRecordRepository.save(record);
        } catch (JsonProcessingException e) {
            throw new RuntimeException("Failed to serialize response payload", e);
        }
    }
}
