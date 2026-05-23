package com.kulu.wallet.controller;

import com.kulu.wallet.domain.Transfer;
import com.kulu.wallet.dto.TransferRequest;
import com.kulu.wallet.repository.TransferRepository;
import com.kulu.wallet.service.TransferService;
import jakarta.validation.Valid;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class TransferController {

    @Autowired private TransferService transferService;
    @Autowired private TransferRepository transferRepo;

    @PostMapping("/transfers")
    public ResponseEntity<Object> createTransfer(@Valid @RequestBody TransferRequest request) {
        return transferService.transfer(request);
    }

    @GetMapping("/transfers/{id}")
    public ResponseEntity<Transfer> getTransfer(@PathVariable String id) {
        return transferRepo.findById(id)
            .map(ResponseEntity::ok)
            .orElse(ResponseEntity.notFound().build());
    }
}
