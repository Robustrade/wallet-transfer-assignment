package com.walletTransfer.walletTransfer.service.impl;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.walletTransfer.walletTransfer.entity.IdempotencyRecord;
    import com.walletTransfer.walletTransfer.entity.Transfer;
import com.walletTransfer.walletTransfer.dto.TransferResponse;
import com.walletTransfer.walletTransfer.exception.IdempotencyConflictException;
import com.walletTransfer.walletTransfer.repository.IdempotencyRecordRepository;
import com.walletTransfer.walletTransfer.service.IdempotencyService;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;

import java.util.Optional;
import java.util.UUID;

@Service
@RequiredArgsConstructor
public class IdempotencyServiceImpl implements IdempotencyService {
    private final IdempotencyRecordRepository repository;
    private final ObjectMapper objectMapper;

    @Override
    public Optional<TransferResponse> findExistingResponse(
            String idempotencyKey,
            String requestHash) {

        return repository
                .findByIdempotencyKey(idempotencyKey)
                .map(record -> {

                    if (!record.getRequestHash()
                            .equals(requestHash)) {

                        throw new IdempotencyConflictException(
                                "Idempotency key reused with different payload");
                    }

                    return deserialize(
                            record.getResponsePayload());
                });
    }

    @Override
    public IdempotencyRecord createPendingRecord(
            String idempotencyKey,
            String requestHash) {

        IdempotencyRecord record =
                IdempotencyRecord.builder()
                        .id(UUID.randomUUID())
                        .idempotencyKey(idempotencyKey)
                        .requestHash(requestHash)
                        .build();

        return repository.save(record);
    }

    @Override
    public void markCompleted(
            IdempotencyRecord record,
            Transfer transfer,
            TransferResponse response) {

        record.markCompleted(
                transfer,
                serialize(response));

        repository.save(record);
    }

    @Override
    public void markFailed(
            IdempotencyRecord record) {

        record.markFailed();

        repository.save(record);
    }

    private String serialize(
            TransferResponse response) {

        try {

            return objectMapper
                    .writeValueAsString(response);

        } catch (JsonProcessingException ex) {

            throw new RuntimeException(ex);
        }
    }

    private TransferResponse deserialize(
            String payload) {

        try {

            return objectMapper.readValue(
                    payload,
                    TransferResponse.class);

        } catch (JsonProcessingException ex) {

            throw new RuntimeException(ex);
        }
    }
}
