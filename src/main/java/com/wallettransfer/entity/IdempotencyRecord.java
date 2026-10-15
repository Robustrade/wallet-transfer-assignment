package com.wallettransfer.entity;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.FetchType;
import jakarta.persistence.ForeignKey;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.GenerationType;
import jakarta.persistence.Id;
import jakarta.persistence.JoinColumn;
import jakarta.persistence.Lob;
import jakarta.persistence.ManyToOne;
import jakarta.persistence.Table;
import jakarta.persistence.UniqueConstraint;
import java.time.Instant;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Getter;
import lombok.NoArgsConstructor;
import lombok.Setter;
import org.hibernate.annotations.CreationTimestamp;

@Entity
@Table(
    name = "idempotency_records",
    uniqueConstraints = {
      @UniqueConstraint(name = "uq_idempotency_key", columnNames = {"idempotency_key"})
    })
@Getter
@Setter
@AllArgsConstructor
@NoArgsConstructor
@Builder
public class IdempotencyRecord {

  @Id
  @GeneratedValue(strategy = GenerationType.IDENTITY)
  private Long id;

  @Column(name = "idempotency_key", nullable = false, length = 128)
  private String idempotencyKey;

  @ManyToOne(fetch = FetchType.LAZY)
  @JoinColumn(
      name = "transfer_id",
      foreignKey = @ForeignKey(name = "fk_idempotency_transfer"))
  private Transfer transfer;

  @Column(name = "request_hash", nullable = false, length = 64)
  private String requestHash;

  @Column(name = "response_status", nullable = false, length = 16)
  private String responseStatus;

  @Column(name = "response_code", nullable = false)
  private Integer responseCode;

  @Column(name = "response_message", nullable = false, length = 512)
  private String responseMessage;

  @Lob
  @Column(name = "response_payload", nullable = false)
  private String responsePayload;

  @CreationTimestamp
  @Column(name = "created_at", nullable = false, updatable = false)
  private Instant createdAt;
}
