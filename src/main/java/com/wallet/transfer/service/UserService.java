package com.wallet.transfer.service;

import com.wallet.transfer.exception.UserExceptionHandler;
import com.wallet.transfer.model.*;
import com.wallet.transfer.repository.UserRepository;
import com.wallet.transfer.repository.WalletRepository;
import org.springframework.security.crypto.bcrypt.BCryptPasswordEncoder;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.math.BigDecimal;
import java.time.LocalDateTime;
import java.util.UUID;

/**
 * Service class for managing user registration and authentication business logic.
 */
@Service
public class UserService {
    
    private final UserRepository userRepository;
    private final WalletRepository walletRepository;
    private final BCryptPasswordEncoder passwordEncoder = new BCryptPasswordEncoder();

    /**
     * Constructs a new UserService with the required repositories.
     *
     * @param userRepository   Repository for managing users.
     * @param walletRepository Repository for managing wallets.
     */
    public UserService(UserRepository userRepository, WalletRepository walletRepository) {
        this.userRepository = userRepository;
        this.walletRepository = walletRepository;
    }

    /**
     * Registers a new user. Generates a new wallet with zero balance for the user,
     * persists both the wallet and the user details in a single transaction, and returns the response.
     *
     * @param request User registration request payload.
     * @return UserResponse containing registration details.
     * @throws UserExceptionHandler if the username, email, or phone number is already registered.
     */
    @Transactional
    public UserResponse register(UserRegisterRequest request) {
        // Check if user already exists
        if (userRepository.findByUsername(request.getUsername()).isPresent()) {
            throw new UserExceptionHandler("Username already exists ! Try another Username ...");
        }
        if (userRepository.findByEmail(request.getEmail()).isPresent()) {
            throw new UserExceptionHandler("Email already exists");
        }
        if (userRepository.findByPhoneno(request.getPhoneno()).isPresent()) {
            throw new UserExceptionHandler("Phone number already exists");
        }

        // Create a new wallet for the user
        String walletId = UUID.randomUUID().toString();
        Wallet wallet = Wallet.builder()
                .id(walletId)
                .balance(BigDecimal.ZERO)
                .createdAt(LocalDateTime.now())
                .build();
        walletRepository.save(wallet);

        // Create the user record
        UserEntry user = UserEntry.builder()
                .username(request.getUsername())
                .email(request.getEmail())
                .phoneno(request.getPhoneno())
                .password(passwordEncoder.encode(request.getPassword())) // Encrypt password using BCrypt
                .walletId(walletId)
                .createdAt(LocalDateTime.now())
                .build();
        
        UserEntry savedUser = userRepository.save(user);

        return UserResponse.builder()
                .id(savedUser.getId())
                .username(savedUser.getUsername())
                .email(savedUser.getEmail())
                .phoneno(savedUser.getPhoneno())
                .walletId(savedUser.getWalletId())
                .message("User registered successfully")
                .build();
    }
    
    /**
     * Authenticates a user using username and password.
     *
     * @param request User login request payload.
     * @return UserResponse containing authenticated user details.
     * @throws UserExceptionHandler if credentials are invalid.
     */
    public UserResponse login(UserLoginRequest request) {
        UserEntry user = userRepository.findByUsername(request.getUsername())
                .orElseThrow(() -> new UserExceptionHandler("Invalid username or password"));

        if (!passwordEncoder.matches(request.getPassword(), user.getPassword())) {
            throw new UserExceptionHandler("Invalid username or password");
        }

        return UserResponse.builder()
                .id(user.getId())
                .username(user.getUsername())
                .email(user.getEmail())
                .phoneno(user.getPhoneno())
                .walletId(user.getWalletId())
                .message("Login successful")
                .build();
    }
}
