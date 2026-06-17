package com.wallet.transfer.model;

import jakarta.persistence.*;
import lombok.*;

import java.time.LocalDateTime;

/**
 * Entity class representing a cached response for an idempotency key.
 * This ensures that duplicate HTTP requests with the same idempotency key are served
 * the same response without re-executing any business logic or mutating the database.
 */
@Entity
@Table(name = "idempotency_records")
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class IdempotencyRecord {

    /**
     * The unique idempotency key supplied by the client.
     */
    @Id
    @Column(name = "idempotency_key", length = 100)
    private String key;

    /**
     * The unique identifier of the transfer result associated with this request.
     */
    @Column(name = "transfer_id", length = 50)
    private String transferId;

    /**
     * The HTTP status code returned to the client (e.g., 201, 400, 422).
     */
    @Column(name = "http_status", nullable = false)
    private int responseStatus;

    /**
     * The serialized JSON response body returned to the client.
     */
    @Column(name = "response_body", nullable = false, length = 5000)
    private String responseBody;

    /**
     * Timestamp indicating when this record was cached.
     */
    @Column(name = "created_at", nullable = false, updatable = false)
    @Builder.Default
    private LocalDateTime createdAt = LocalDateTime.now();
}
