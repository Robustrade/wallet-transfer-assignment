package com.videtech.wallet_transfer.domain;

import java.math.BigDecimal;
import java.time.LocalDate;
import java.time.LocalDateTime;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.PrePersist;
import jakarta.persistence.PreUpdate;
import jakarta.persistence.Table;
import lombok.Getter;
import lombok.Setter;

@Entity
@Table(name ="transfers")
@Getter @Setter
public class Transfer {

	@Id
	private String id;
	
	@Column(name="idempotency_key",nullable=false,unique=true)
	private String idempotencyKey;
	@Column(name="from_wallet_id",nullable=false)
	private String fromWalletId;
	@Column(name="to_wallet_id",nullable=false)
	private String toWalletId;
	@Column(nullable = false, precision = 19, scale = 4)
	private BigDecimal amount;
	
    @Enumerated(EnumType.STRING)
    @Column(nullable = false)
    private TransferStatus status;
    
    @Column(name = "response_body", columnDefinition = "TEXT")
    private String responseBody;

    @Column(name = "created_at")
    private LocalDateTime createdAt;
    
    @Column(name = "updated_at")
    private LocalDateTime updatedAt;
	
    @PrePersist
    public void prePersist() {
        createdAt = LocalDateTime.now();
        updatedAt = LocalDateTime.now();
    }
	
    @PreUpdate
    public void preUpdate() {
        updatedAt = LocalDateTime.now();
    }
	
}
