package com.wallet.domain;

import java.time.Instant;

/**
 * Registered API caller. Requests must carry an {@code X-Client-Id} that
 * resolves to a row here; the table is also the FK target for
 * {@code idempotency_records.client_id}.
 */
public class Client {

    private final String id;
    private final String name;
    private final Instant createdAt;

    public Client(String id, String name, Instant createdAt) {
        this.id = id;
        this.name = name;
        this.createdAt = createdAt;
    }

    public String getId() {
        return id;
    }

    public String getName() {
        return name;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }
}
