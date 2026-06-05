package com.example.wallet.handler.dto.validation;

import com.example.wallet.handler.dto.CreateTransferRequest;
import jakarta.validation.ConstraintValidator;
import jakarta.validation.ConstraintValidatorContext;

public class NotSelfTransferValidator implements ConstraintValidator<NotSelfTransfer, CreateTransferRequest> {

    @Override
    public boolean isValid(CreateTransferRequest request, ConstraintValidatorContext context) {
        if (request == null) {
            return true; // null handled by @NotNull elsewhere
        }
        if (request.fromWalletId() == null || request.toWalletId() == null) {
            return true; // blank/null handled by @NotBlank elsewhere
        }
        return !request.fromWalletId().equals(request.toWalletId());
    }
}

