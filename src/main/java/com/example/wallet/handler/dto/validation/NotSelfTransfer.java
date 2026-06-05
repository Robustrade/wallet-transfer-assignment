package com.example.wallet.handler.dto.validation;

import jakarta.validation.Constraint;
import jakarta.validation.Payload;

import java.lang.annotation.Documented;
import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

/**
 * Class-level constraint that rejects transfers where fromWalletId equals toWalletId.
 * Applied on CreateTransferRequest so it is caught at the handler boundary
 * before any service or repository logic runs.
 */
@Documented
@Constraint(validatedBy = NotSelfTransferValidator.class)
@Target(ElementType.TYPE)
@Retention(RetentionPolicy.RUNTIME)
public @interface NotSelfTransfer {

    String message() default "fromWalletId and toWalletId must not be the same";

    Class<?>[] groups() default {};

    Class<? extends Payload>[] payload() default {};
}

