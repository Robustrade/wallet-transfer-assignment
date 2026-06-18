package com.wallet.domain;

import java.math.BigDecimal;
import java.math.RoundingMode;

/**
 * Canonical scale for all monetary amounts, matching the NUMERIC(19,4) ledger columns. Without
 * this, the same amount can round-trip with a different {@link BigDecimal} scale depending on
 * whether it came straight from a JSON request or was reloaded from the database, producing
 * inconsistent API responses (e.g. {@code 100} vs {@code 100.0000}) even though the values are
 * numerically equal.
 */
public final class Money {

    private static final int SCALE = 4;

    private Money() {
    }

    public static BigDecimal normalize(BigDecimal amount) {
        return amount.setScale(SCALE, RoundingMode.HALF_UP);
    }
}
