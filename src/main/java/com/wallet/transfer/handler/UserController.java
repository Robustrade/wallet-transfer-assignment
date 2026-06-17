package com.wallet.transfer.handler;

import com.wallet.transfer.model.UserLoginRequest;
import com.wallet.transfer.model.UserRegisterRequest;
import com.wallet.transfer.model.UserResponse;
import com.wallet.transfer.service.UserService;
import jakarta.validation.Valid;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

/**
 * REST controller for handling user registration and login/authentication endpoints.
 */
@RestController
@RequestMapping("/user")
public class UserController {

    private final UserService userService;

    /**
     * Constructs a new UserController with the required UserService dependency.
     *
     * @param userService The service containing user management business logic.
     */
    public UserController(UserService userService) {
        this.userService = userService;
    }

    /**
     * Endpoint to register a new user and automatically create a wallet.
     *
     * @param request Validated user registration request payload.
     * @return ResponseEntity containing user details and HTTP 201 Created status.
     */
    @PostMapping("/register")
    public ResponseEntity<UserResponse> register(@Valid @RequestBody UserRegisterRequest request) {
        UserResponse response = userService.register(request);
        return new ResponseEntity<>(response, HttpStatus.CREATED);
    }

    /**
     * Endpoint to authenticate an existing user.
     *
     * @param request Validated user login request payload.
     * @return ResponseEntity containing user details and HTTP 200 OK status.
     */
    @PostMapping("/login")
    public ResponseEntity<UserResponse> login(@Valid @RequestBody UserLoginRequest request) {
        UserResponse response = userService.login(request);
        return ResponseEntity.ok(response);
    }
}
