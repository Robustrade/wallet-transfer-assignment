package com.mycompany.wallet_transaction.controller;

import com.mycompany.wallet_transaction.dto.PaymentRequestDto;
import com.mycompany.wallet_transaction.dto.PaymentResponseDto;
import com.mycompany.wallet_transaction.service.PaymentService;
import jakarta.validation.Valid;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class PaymentRequestController {

    PaymentService paymentService;

    public PaymentRequestController(PaymentService paymentService) {
        this.paymentService = paymentService;
    }

    @PostMapping("/transfers")
    public ResponseEntity<PaymentResponseDto> makeTransfer(@Valid @RequestBody PaymentRequestDto paymentRequest){
        PaymentResponseDto response = paymentService.makeTransfer(paymentRequest);
        return ResponseEntity.ok(response);
    }
}
