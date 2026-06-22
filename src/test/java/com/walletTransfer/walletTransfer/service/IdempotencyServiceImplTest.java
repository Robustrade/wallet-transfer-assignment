package com.walletTransfer.walletTransfer.service;


import com.fasterxml.jackson.databind.ObjectMapper;
import com.walletTransfer.walletTransfer.dto.TransferResponse;
import com.walletTransfer.walletTransfer.entity.IdempotencyRecord;
import com.walletTransfer.walletTransfer.entity.Transfer;
import com.walletTransfer.walletTransfer.exception.IdempotencyConflictException;
import com.walletTransfer.walletTransfer.repository.IdempotencyRecordRepository;
import com.walletTransfer.walletTransfer.service.impl.IdempotencyServiceImpl;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import java.util.Optional;
import java.util.UUID;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class IdempotencyServiceImplTest {


    @Mock
    private IdempotencyRecordRepository repository;

    @Mock
    private ObjectMapper objectMapper;

    @InjectMocks
    private IdempotencyServiceImpl idempotencyService;

    @Test
    void shouldReturnExistingResponse() throws Exception {

        TransferResponse response =
                TransferResponse.builder()
                        .build();

        IdempotencyRecord record =
                IdempotencyRecord.builder()
                        .id(UUID.randomUUID())
                        .idempotencyKey("key")
                        .requestHash("hash")
                        .responsePayload("{\"status\":\"COMPLETED\"}")
                        .build();

        when(repository.findByIdempotencyKey("key"))
                .thenReturn(Optional.of(record));

        when(objectMapper.readValue(
                record.getResponsePayload(),
                TransferResponse.class))
                .thenReturn(response);

        Optional<TransferResponse> result =
                idempotencyService.findExistingResponse(
                        "key",
                        "hash");

        assertTrue(result.isPresent());
        assertEquals(response, result.get());
    }

    @Test
    void shouldThrowConflictExceptionWhenHashDiffers() {

        IdempotencyRecord record =
                IdempotencyRecord.builder()
                        .requestHash("oldHash")
                        .build();

        when(repository.findByIdempotencyKey("key"))
                .thenReturn(Optional.of(record));

        assertThrows(
                IdempotencyConflictException.class,
                () -> idempotencyService.findExistingResponse(
                        "key",
                        "newHash")
        );
    }

    @Test
    void shouldCreatePendingRecord() {

        IdempotencyRecord savedRecord =
                IdempotencyRecord.builder()
                        .id(UUID.randomUUID())
                        .idempotencyKey("key")
                        .requestHash("hash")
                        .build();

        when(repository.save(any(IdempotencyRecord.class)))
                .thenReturn(savedRecord);

        IdempotencyRecord result =
                idempotencyService.createPendingRecord(
                        "key",
                        "hash");

        assertNotNull(result);
        assertEquals("key", result.getIdempotencyKey());
    }

    @Test
    void shouldMarkCompleted() {

        IdempotencyRecord record =
                mock(IdempotencyRecord.class);

        Transfer transfer =
                mock(Transfer.class);

        TransferResponse response =
                TransferResponse.builder()
                        .build();

        when(repository.save(record))
                .thenReturn(record);

        try {
            when(objectMapper.writeValueAsString(response))
                    .thenReturn("{\"status\":\"COMPLETED\"}");
        } catch (Exception ex) {
            fail();
        }

        idempotencyService.markCompleted(
                record,
                transfer,
                response);

        verify(record)
                .markCompleted(
                        eq(transfer),
                        anyString());

        verify(repository)
                .save(record);
    }

    @Test
    void shouldMarkFailed() {

        IdempotencyRecord record =
                mock(IdempotencyRecord.class);

        idempotencyService.markFailed(record);

        verify(record)
                .markFailed();

        verify(repository)
                .save(record);
    }


}

