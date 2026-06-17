package com.wallet.transfer.model;

import jakarta.persistence.*;
import lombok.*;

import java.time.LocalDateTime;

/**
 * Entity class representing a user record in the system.
 * Contains user details including registration information and references the user's wallet.
 */
@Entity
@Table(name = "users")
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class UserEntry {

    /**
     * Unique identifier for the user.
     */
    @Id
    @Column(name = "id")
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    /**
     * ID of the wallet associated with this user.
     */
    @Column(name = "wallet_id", nullable = false, length = 50)
    private String walletId;

    /**
     * The username of the user.
     */
    @Column(name = "username", nullable = false, length = 100)
    private String username;

    /**
     * The email address of the user.
     */
    @Column(name = "email", nullable = false, length = 100)
    private String email;

    /**
     * The phone number of the user.
     */
    @Column(name = "phoneno", nullable = false, length = 15)
    private String phoneno;

    /**
     * The encrypted password of the user.
     */
    @Column(name = "password", nullable = false, length = 255)
    private String password;

    /**
     * Timestamp indicating when this user was registered.
     */
    @Column(name = "created_at", nullable = false, updatable = false)
    @Builder.Default
    private LocalDateTime createdAt = LocalDateTime.now();
}

