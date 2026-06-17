package com.wallet.transfer;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.transfer.model.*;
import com.wallet.transfer.repository.*;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.http.MediaType;
import org.springframework.security.crypto.bcrypt.BCryptPasswordEncoder;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.MvcResult;

import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.*;

import static org.hamcrest.Matchers.is;
import static org.junit.jupiter.api.Assertions.*;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

@SpringBootTest
@AutoConfigureMockMvc
public class WalletTransferApplicationTests {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private UserRepository userRepository;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @Autowired
    private IdempotencyRecordRepository idempotencyRecordRepository;

    @Autowired
    private ObjectMapper objectMapper;

    private final BCryptPasswordEncoder passwordEncoder = new BCryptPasswordEncoder();

    @BeforeEach
    public void setup() {
        // Clear all previous test data in correct dependency order
        userRepository.deleteAll();
        idempotencyRecordRepository.deleteAll();
        ledgerEntryRepository.deleteAll();
        transferRepository.deleteAll();
        walletRepository.deleteAll();

        // Create initial wallets
        walletRepository.save(Wallet.builder().id("wallet_1").balance(new BigDecimal("1000.0000")).build());
        walletRepository.save(Wallet.builder().id("wallet_2").balance(new BigDecimal("500.0000")).build());
        walletRepository.save(Wallet.builder().id("wallet_3").balance(new BigDecimal("0.0000")).build());
    }

    @Test
    public void testSuccessfulTransfer() throws Exception {
        TransferRequest request = new TransferRequest("key-success-1", "wallet_1", "wallet_2", new BigDecimal("100.00"));

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.status", is("PROCESSED")))
                .andExpect(jsonPath("$.fromWalletId", is("wallet_1")))
                .andExpect(jsonPath("$.toWalletId", is("wallet_2")))
                .andExpect(jsonPath("$.amount", is(100.00)));

        // Verify database state
        Wallet fromWallet = walletRepository.findById("wallet_1").orElseThrow();
        Wallet toWallet = walletRepository.findById("wallet_2").orElseThrow();

        assertEquals(new BigDecimal("900.0000"), fromWallet.getBalance());
        assertEquals(new BigDecimal("600.0000"), toWallet.getBalance());

        // Verify ledger entries
        List<Transfer> transfers = transferRepository.findAll();
        assertEquals(1, transfers.size());
        Transfer transfer = transfers.get(0);
        assertEquals("PROCESSED", transfer.getStatus().name());

        List<LedgerEntry> ledgerEntries = ledgerEntryRepository.findByTransferId(transfer.getId());
        assertEquals(2, ledgerEntries.size());

        LedgerEntry debit = ledgerEntries.stream().filter(e -> e.getType() == LedgerEntryType.DEBIT).findFirst().orElseThrow();
        LedgerEntry credit = ledgerEntries.stream().filter(e -> e.getType() == LedgerEntryType.CREDIT).findFirst().orElseThrow();

        assertEquals("wallet_1", debit.getWalletId());
        assertEquals(new BigDecimal("100.0000"), debit.getAmount());

        assertEquals("wallet_2", credit.getWalletId());
        assertEquals(new BigDecimal("100.0000"), credit.getAmount());
    }

    @Test
    public void testIdempotencyOfSuccessfulTransfer() throws Exception {
        TransferRequest request = new TransferRequest("key-idem-1", "wallet_1", "wallet_2", new BigDecimal("200.00"));

        // First request
        MvcResult result1 = mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isCreated())
                .andReturn();

        String responseContent1 = result1.getResponse().getContentAsString();

        // Second request with same idempotency key
        MvcResult result2 = mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isCreated())
                .andReturn();

        String responseContent2 = result2.getResponse().getContentAsString();

        // Verify both responses are identical
        assertEquals(responseContent1, responseContent2);

        // Verify balance was only deducted once
        Wallet fromWallet = walletRepository.findById("wallet_1").orElseThrow();
        assertEquals(new BigDecimal("800.0000"), fromWallet.getBalance());

        // Verify only 1 transfer and 2 ledger entries exist
        assertEquals(1, transferRepository.findAll().size());
        assertEquals(2, ledgerEntryRepository.findAll().size());
    }

    @Test
    public void testInsufficientFunds() throws Exception {
        TransferRequest request = new TransferRequest("key-fail-1", "wallet_3", "wallet_1", new BigDecimal("10.00"));

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isUnprocessableEntity())
                .andExpect(jsonPath("$.error", is("Insufficient funds in wallet: wallet_3")));

        // Verify database state is unchanged
        Wallet fromWallet = walletRepository.findById("wallet_3").orElseThrow();
        assertEquals(new BigDecimal("0.0000"), fromWallet.getBalance());

        // Verify transfer record was created as FAILED
        List<Transfer> transfers = transferRepository.findAll();
        assertEquals(1, transfers.size());
        assertEquals(TransferStatus.FAILED, transfers.get(0).getStatus());

        // Verify no ledger entries were created
        assertEquals(0, ledgerEntryRepository.findAll().size());
    }

    @Test
    public void testSelfTransferRejected() throws Exception {
        TransferRequest request = new TransferRequest("key-self-1", "wallet_1", "wallet_1", new BigDecimal("100.00"));

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.error", is("Cannot transfer to the same wallet")));
    }

    @Test
    public void testConcurrentTransfersFromSameSender() throws Exception {
        int threadsCount = 10;
        BigDecimal transferAmount = new BigDecimal("100.00"); // 10 transfers of 100 from balance of 1000

        ExecutorService executor = Executors.newFixedThreadPool(threadsCount);
        CountDownLatch latch = new CountDownLatch(1);
        List<Future<MvcResult>> futures = new ArrayList<>();

        for (int i = 0; i < threadsCount; i++) {
            final String key = "key-concurrent-" + i;
            TransferRequest request = new TransferRequest(key, "wallet_1", "wallet_2", transferAmount);

            Callable<MvcResult> task = () -> {
                latch.await(); // Wait for the latch to release to start all requests at once
                return mockMvc.perform(post("/transfers")
                                .contentType(MediaType.APPLICATION_JSON)
                                .content(objectMapper.writeValueAsString(request)))
                        .andReturn();
            };
            futures.add(executor.submit(task));
        }

        // Start all threads simultaneously
        latch.countDown();

        for (Future<MvcResult> future : futures) {
            MvcResult result = future.get();
            int status = result.getResponse().getStatus();
            // All 10 requests should succeed because 10 * 100 = 1000, which is exactly wallet_1's balance
            assertEquals(201, status);
        }

        executor.shutdown();

        // Verify final balances
        Wallet fromWallet = walletRepository.findById("wallet_1").orElseThrow();
        Wallet toWallet = walletRepository.findById("wallet_2").orElseThrow();

        assertEquals(new BigDecimal("0.0000"), fromWallet.getBalance());
        assertEquals(new BigDecimal("1500.0000"), toWallet.getBalance());

        // Verify ledger consistency
        assertEquals(10, transferRepository.findAll().size());
        assertEquals(20, ledgerEntryRepository.findAll().size());
    }

    @Test
    public void testUserRegistrationSuccess() throws Exception {
        UserRegisterRequest request = UserRegisterRequest.builder()
                .username("john_doe")
                .email("john@example.com")
                .phoneno("1234567890")
                .password("password123")
                .build();

        mockMvc.perform(post("/user/register")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.username", is("john_doe")))
                .andExpect(jsonPath("$.email", is("john@example.com")))
                .andExpect(jsonPath("$.phoneno", is("1234567890")))
                .andExpect(jsonPath("$.walletId").exists())
                .andExpect(jsonPath("$.message", is("User registered successfully")));

        // Verify user and wallet created in database
        UserEntry user = userRepository.findByUsername("john_doe").orElseThrow();
        assertNotNull(user.getWalletId());

        Wallet wallet = walletRepository.findById(user.getWalletId()).orElseThrow();
        assertEquals(BigDecimal.ZERO.setScale(4), wallet.getBalance().setScale(4));
    }

    @Test
    public void testUserRegistrationDuplicateUsername() throws Exception {
        // Save an initial user
        userRepository.save(UserEntry.builder()
                .username("john_doe")
                .email("old@example.com")
                .phoneno("0987654321")
                .password(passwordEncoder.encode("pass1"))
                .walletId("wallet_1")
                .build());

        UserRegisterRequest request = UserRegisterRequest.builder()
                .username("john_doe") // Duplicate
                .email("john@example.com")
                .phoneno("1234567890")
                .password("password123")
                .build();

        mockMvc.perform(post("/user/register")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.error", is("Username already exists ! Try another Username ...")));
    }

    @Test
    public void testUserLoginSuccess() throws Exception {
        // Save an initial user
        userRepository.save(UserEntry.builder()
                .username("john_doe")
                .email("john@example.com")
                .phoneno("1234567890")
                .password(passwordEncoder.encode("password123"))
                .walletId("wallet_1")
                .build());

        UserLoginRequest request = new UserLoginRequest("john_doe", "password123");

        mockMvc.perform(post("/user/login")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.username", is("john_doe")))
                .andExpect(jsonPath("$.walletId", is("wallet_1")))
                .andExpect(jsonPath("$.message", is("Login successful")));
    }

    @Test
    public void testUserLoginWrongPassword() throws Exception {
        // Save an initial user
        userRepository.save(UserEntry.builder()
                .username("john_doe")
                .email("john@example.com")
                .phoneno("1234567890")
                .password(passwordEncoder.encode("password123"))
                .walletId("wallet_1")
                .build());

        UserLoginRequest request = new UserLoginRequest("john_doe", "wrong_pass");

        mockMvc.perform(post("/user/login")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.error", is("Invalid username or password")));
    }
}

