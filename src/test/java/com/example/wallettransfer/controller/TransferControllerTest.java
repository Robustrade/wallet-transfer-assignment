package com.example.wallettransfer.controller;

import com.example.wallettransfer.domain.TransferStatus;
import com.example.wallettransfer.dto.CreateTransferRequest;
import com.example.wallettransfer.dto.TransferResponse;
import com.example.wallettransfer.service.TransferService;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;
import org.springframework.http.HttpStatus;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

class TransferControllerTest {

    @Test
    void returnsServiceResultWithOkStatus() {
        TransferService service = mock(TransferService.class);
        TransferController controller = new TransferController(service);
        CreateTransferRequest request = new CreateTransferRequest("key", "source", "destination", BigDecimal.TEN);
        TransferResponse expected = new TransferResponse(UUID.randomUUID(), "key", "source", "destination",
                BigDecimal.TEN, TransferStatus.PROCESSED, null, Instant.EPOCH, Instant.EPOCH);
        when(service.createTransfer(request)).thenReturn(expected);

        var response = controller.createTransfer(request);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
        assertThat(response.getBody()).isEqualTo(expected);
        ArgumentCaptor<CreateTransferRequest> passedRequest = ArgumentCaptor.forClass(CreateTransferRequest.class);
        verify(service).createTransfer(passedRequest.capture());
        assertThat(passedRequest.getValue()).isEqualTo(request);
    }
}
