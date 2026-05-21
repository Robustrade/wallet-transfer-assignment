package com.kullu.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

class RequestHasherTest {

    @Test
    void sameInputsProduceSameHash() {
        String from = "wallet-a";
        String to = "wallet-b";

        assertThat(RequestHasher.hash(from, to, 100))
            .isEqualTo(RequestHasher.hash(from, to, 100))
            .hasSize(64);
    }

    @Test
    void differentAmountsProduceDifferentHashes() {
        String from = "wallet-a";
        String to = "wallet-b";

        assertThat(RequestHasher.hash(from, to, 100))
            .isNotEqualTo(RequestHasher.hash(from, to, 101));
    }

    @Test
    void swappingFromAndToProducesDifferentHash() {
        String a = "wallet-a";
        String b = "wallet-b";

        // Direction matters — A→B is not the same request as B→A.
        assertThat(RequestHasher.hash(a, b, 100))
            .isNotEqualTo(RequestHasher.hash(b, a, 100));
    }

    /**
     * Regression test for the delimiter-injection collision flagged in PR #38.
     * A naive {@code from + "|" + to} canonicalisation would hash
     * {@code ("a|b", "c", 100)} identically to {@code ("a", "b|c", 100)}.
     * Length-prefixed framing must keep these distinct.
     */
    @Test
    void delimiterInjectionCannotForgeCollision() {
        assertThat(RequestHasher.hash("a|b", "c", 100))
            .isNotEqualTo(RequestHasher.hash("a", "b|c", 100));
        // Also guard against amount-side collisions
        // ("100" appended to "from|to" vs "100" as its own field).
        assertThat(RequestHasher.hash("from", "to|100", 0))
            .isNotEqualTo(RequestHasher.hash("from", "to", 100));
    }
}
