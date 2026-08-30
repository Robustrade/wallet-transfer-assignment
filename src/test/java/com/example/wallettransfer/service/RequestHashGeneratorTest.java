package com.example.wallettransfer.service;

import com.example.wallettransfer.dto.CreateTransferRequest;
import com.example.wallettransfer.util.RequestHashGenerator;
import org.junit.jupiter.api.Test;

import java.math.BigDecimal;

import static org.assertj.core.api.Assertions.assertThat;

class RequestHashGeneratorTest {

    private final RequestHashGenerator generator = new RequestHashGenerator();

    @Test
    void equivalentDecimalRepresentationsProduceSameHash() {
        CreateTransferRequest first = new CreateTransferRequest("key-a", "w1", "w2", new BigDecimal("100.0"));
        CreateTransferRequest second = new CreateTransferRequest("key-b", "w1", "w2", new BigDecimal("100.0000"));

        assertThat(generator.hash(first)).isEqualTo(generator.hash(second));
    }

    @Test
    void idempotencyKeyIsNotPartOfRequestFingerprint() {
        CreateTransferRequest first = new CreateTransferRequest("key-a", "w1", "w2", new BigDecimal("100"));
        CreateTransferRequest second = new CreateTransferRequest("key-b", "w1", "w2", new BigDecimal("100"));

        assertThat(generator.hash(first)).isEqualTo(generator.hash(second));
    }

    @Test
    void changedBusinessPayloadProducesDifferentHash() {
        CreateTransferRequest first = new CreateTransferRequest("key", "w1", "w2", new BigDecimal("100"));
        CreateTransferRequest second = new CreateTransferRequest("key", "w1", "w2", new BigDecimal("101"));

        assertThat(generator.hash(first)).isNotEqualTo(generator.hash(second));
    }

    @Test
    void distinctPayloadsContainingDelimiterCharactersProduceDifferentHashes() {
        CreateTransferRequest first = new CreateTransferRequest("key", "a\nb", "c", new BigDecimal("100"));
        CreateTransferRequest second = new CreateTransferRequest("key", "a", "b\nc", new BigDecimal("100"));

        assertThat(generator.hash(first)).isNotEqualTo(generator.hash(second));
    }
}
