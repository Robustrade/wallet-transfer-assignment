package com.wallet.transfer.model;

import jakarta.validation.constraints.NotBlank;
import lombok.*;

/**
 * Request payload for user login.
 */
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class UserLoginRequest {

    @NotBlank(message = "Username is required")
    private String username;

    @NotBlank(message = "Password is required")
    private String password;
}
