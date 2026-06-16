package com.viditec.wallettransfer.api.controller;

import com.viditec.wallettransfer.api.request.CreateTransferRequest;
import com.viditec.wallettransfer.dto.TransferDto;
import com.viditec.wallettransfer.mapper.ApiMapper;
import com.viditec.wallettransfer.service.TransferService;
import jakarta.validation.Valid;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/transfers")
public class TransferController {

    private final TransferService transferService;
    private final ApiMapper apiMapper;

    public TransferController(TransferService transferService, ApiMapper apiMapper) {
        this.transferService = transferService;
        this.apiMapper = apiMapper;
    }

    @PostMapping
    public ResponseEntity<?> createTransfer(@Valid @RequestBody CreateTransferRequest request) {
        ResponseEntity<?> response = transferService.createTransfer(apiMapper.toDto(request));
        if (response.getBody() instanceof TransferDto transferDto) {
            return ResponseEntity.status(response.getStatusCode()).body(apiMapper.toResponse(transferDto));
        }
        return response;
    }
}
