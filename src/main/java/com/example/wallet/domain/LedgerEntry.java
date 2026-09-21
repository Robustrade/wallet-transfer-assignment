package com.example.wallet.domain;

import jakarta.persistence.*;
import org.hibernate.annotations.Immutable;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

@Entity
@Immutable
@Table(
        name = "ledger_entries",
        uniqueConstraints = @UniqueConstraint(
                name = "uk_ledger_transfer_wallet_type",
                columnNames = {
                        "transfer_id",
                        "wallet_id",
                        "entry_type"
                }
        ),
        indexes = {
                @Index(
                        name = "idx_ledger_wallet_created",
                        columnList = "wallet_id, created_at DESC"
                )
        }
)
public class LedgerEntry {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    @Column(
            name = "wallet_id",
            nullable = false,
            length = 100
    )
    private String walletId;

    @Column(
            name = "transfer_id",
            nullable = false
    )
    private UUID transferId;

    @Enumerated(EnumType.STRING)
    @Column(
            name = "entry_type",
            nullable = false,
            length = 20
    )
    private LedgerEntryType type;

    @Column(
            name = "amount",
            nullable = false,
            precision = 19,
            scale = 2
    )
    private BigDecimal amount;

    @Column(
            name = "created_at",
            nullable = false
    )
    private Instant createdAt;

    /**
     * Required by JPA.
     */
    protected LedgerEntry() {
    }

    /**
     * Constructor used when creating a ledger entry.
     */
    public LedgerEntry(
            String walletId,
            UUID transferId,
            LedgerEntryType type,
            BigDecimal amount
    ) {
        this.walletId = walletId;
        this.transferId = transferId;
        this.type = type;
        this.amount = amount;
        this.createdAt = Instant.now();
    }

    public Long getId() {
        return id;
    }

    public String getWalletId() {
        return walletId;
    }

    public UUID getTransferId() {
        return transferId;
    }

    public LedgerEntryType getType() {
        return type;
    }

    public BigDecimal getAmount() {
        return amount;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }

    public static LedgerEntry debit(
            String walletId,
            UUID transferId,
            BigDecimal amount) {

        return new LedgerEntry(
                walletId,
                transferId,
                LedgerEntryType.DEBIT,
                amount
        );
    }

    public static LedgerEntry credit(
            String walletId,
            UUID transferId,
            BigDecimal amount) {

        return new LedgerEntry(
                walletId,
                transferId,
                LedgerEntryType.CREDIT,
                amount
        );
    }
}