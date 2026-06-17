package com.wallet.transfer.model;

import lombok.*;

/**
 * Response payload containing user details (excluding sensitive data like password).
 */
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class UserResponse {
    private Long id;
    private String username;
    private String email;
    private String phoneno;
    private String walletId;
    private String message;
}
