package com.wallet.controller.dto;

import com.fasterxml.jackson.annotation.JsonIgnore;
import jakarta.validation.constraints.AssertTrue;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Positive;

/** Transfer creation payload; amount is in cents. */
public record CreateTransferRequest(
        @NotBlank(message = "fromWalletId is required")
        String fromWalletId,

        @NotBlank(message = "toWalletId is required")
        String toWalletId,

        @NotNull(message = "amount is required")
        @Positive(message = "amount must be positive")
        Long amount
) {
    /**
     * Cross-field rule lives on the DTO rather than in a separate validator
     * class so it rides the same JSR-303 validation pass as the field-level
     * constraints and surfaces through the standard
     * {@code MethodArgumentNotValidException} → 400 path. A standalone
     * validator was considered but rejected: it would split the request-
     * validation surface in two and require its own exception type and
     * handler, with no offsetting benefit. The synthetic
     * {@code walletPairValid} property name is implementation detail
     * (Hibernate Validator derives it from the accessor); the
     * {@link com.wallet.exception.GlobalExceptionHandler} emits only the
     * configured {@code message} so the field name does not leak to the wire.
     *
     * <p>The accessor is named {@code isWalletPairValid} rather than the
     * more obvious {@code isDifferentWallets}: returning {@code true} when
     * either side is {@code null} reads incorrectly under the latter, since
     * the predicate's actual semantic is "the pair is valid for the
     * cross-field check" — either both fields are populated and distinct, or
     * one is null and another field-level constraint will surface the real
     * 400 first. The chosen name pins that intent.
     *
     * <p>Returns {@code true} when either side is {@code null} so this rule
     * does not fire in addition to {@link NotBlank}; the null cases are
     * already covered by their own field-level errors.
     */
    @JsonIgnore
    @AssertTrue(message = "Source and destination wallet cannot be the same")
    public boolean isWalletPairValid() {
        if (fromWalletId == null || toWalletId == null) {
            return true;
        }
        return !fromWalletId.equals(toWalletId);
    }

    /**
     * Canonical string for duplicate-detection hashing. JSON quoting prevents
     * collisions a naive separator (e.g. '|') would cause if a wallet id
     * happened to contain that character.
     */
    public String toCanonicalString() {
        return "{\"fromWalletId\":" + jsonString(fromWalletId)
                + ",\"toWalletId\":" + jsonString(toWalletId)
                + ",\"amount\":" + amount + "}";
    }

    private static final char[] HEX = "0123456789abcdef".toCharArray();

    private static String jsonString(String s) {
        if (s == null) return "null";
        StringBuilder sb = new StringBuilder(s.length() + 2);
        sb.append('"');
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '"':  sb.append("\\\""); break;
                case '\\': sb.append("\\\\"); break;
                case '\b': sb.append("\\b");  break;
                case '\f': sb.append("\\f");  break;
                case '\n': sb.append("\\n");  break;
                case '\r': sb.append("\\r");  break;
                case '\t': sb.append("\\t");  break;
                default:
                    if (c < 0x20) {
                        // Hand-rolled hex emit — String.format("\\u%04x", c)
                        // would parse the format string, allocate a Formatter,
                        // and produce garbage on every control char. Wallet IDs
                        // shouldn't contain these, but a buggy upstream that
                        // smuggles one shouldn't pay the per-call Formatter cost.
                        sb.append("\\u00")
                          .append(HEX[(c >> 4) & 0xF])
                          .append(HEX[c & 0xF]);
                    } else {
                        sb.append(c);
                    }
            }
        }
        sb.append('"');
        return sb.toString();
    }
}
