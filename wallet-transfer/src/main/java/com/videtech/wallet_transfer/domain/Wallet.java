package com.videtech.wallet_transfer.domain;

import java.math.BigDecimal;
import java.time.LocalDateTime;

import jakarta.persistence.*;
import lombok.Getter;
import lombok.Setter;

@Entity
@Table(name="wallets")
@Getter 
@Setter
public class Wallet {

	@Id
	private String id;
	@Column(nullable = false, precision = 19, scale = 4)
	private BigDecimal balance;
	@Version
	private Long version;
	@Column(name="created_at")
	private LocalDateTime createdAt;
	@PrePersist
	public void prePersist() {
		 createdAt = LocalDateTime.now();
	}
}
