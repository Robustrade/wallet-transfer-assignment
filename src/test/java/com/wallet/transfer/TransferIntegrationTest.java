package com.wallet.transfer;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.transfer.dto.TransferRequest;
import com.wallet.transfer.entity.Wallet;
import com.wallet.transfer.repository.WalletRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.http.MediaType;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;
import org.springframework.test.web.servlet.MockMvc;
import org.testcontainers.containers.PostgreSQLContainer;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;

import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

@SpringBootTest
@AutoConfigureMockMvc
@Testcontainers
public class TransferIntegrationTest {

    @Container
    public static PostgreSQLContainer<?> postgreSQLContainer = new PostgreSQLContainer<>("postgres:15-alpine")
            .withDatabaseName("wallet_db")
            .withUsername("wallet_user")
            .withPassword("wallet_password");

    @DynamicPropertySource
    static void registerPgProperties(DynamicPropertyRegistry registry) {
        registry.add("spring.datasource.url", postgreSQLContainer::getJdbcUrl);
        registry.add("spring.datasource.username", postgreSQLContainer::getUsername);
        registry.add("spring.datasource.password", postgreSQLContainer::getPassword);
    }

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private ObjectMapper objectMapper;

    @Autowired
    private WalletRepository walletRepository;

    @BeforeEach
    void setUp() {
        // Flyway migrations have populated wallet_1 (1000.00) and wallet_2 (500.00).
        // Let's reset them before each test to ensure predictable state.
        Wallet w1 = walletRepository.findById("wallet_1").orElseThrow();
        w1.setBalance(new BigDecimal("1000.00"));
        walletRepository.save(w1);

        Wallet w2 = walletRepository.findById("wallet_2").orElseThrow();
        w2.setBalance(new BigDecimal("500.00"));
        walletRepository.save(w2);
    }

    @Test
    void testIdempotency() throws Exception {
        TransferRequest request = new TransferRequest();
        request.setIdempotencyKey("idem-123");
        request.setFromWalletId("wallet_1");
        request.setToWalletId("wallet_2");
        request.setAmount(new BigDecimal("100.00"));

        String payload = objectMapper.writeValueAsString(request);

        // First request
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(payload))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.state").value("PROCESSED"));

        // Second request with same idempotency key
        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(payload))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.state").value("PROCESSED"));

        // Balance should only be deducted once
        Wallet w1 = walletRepository.findById("wallet_1").orElseThrow();
        assertEquals(new BigDecimal("900.00"), w1.getBalance());
    }

    @Test
    void testConcurrencySafety() throws Exception {
        int threadCount = 10;
        ExecutorService executorService = Executors.newFixedThreadPool(threadCount);
        CountDownLatch latch = new CountDownLatch(threadCount);
        List<Future<Void>> futures = new ArrayList<>();

        for (int i = 0; i < threadCount; i++) {
            final int index = i;
            futures.add(executorService.submit(() -> {
                try {
                    TransferRequest request = new TransferRequest();
                    request.setIdempotencyKey("concurrent-key-" + index);
                    request.setFromWalletId("wallet_1"); // Initial balance 1000
                    request.setToWalletId("wallet_2");
                    request.setAmount(new BigDecimal("100.00"));

                    mockMvc.perform(post("/transfers")
                                    .contentType(MediaType.APPLICATION_JSON)
                                    .content(objectMapper.writeValueAsString(request)))
                            .andExpect(status().isCreated());
                } finally {
                    latch.countDown();
                }
                return null;
            }));
        }

        boolean completed = latch.await(10, TimeUnit.SECONDS);
        assertEquals(true, completed, "Timeout waiting for threads to finish");
        
        executorService.shutdown();
        executorService.awaitTermination(5, TimeUnit.SECONDS);

        // Propagate any exceptions that happened in threads
        for (Future<Void> future : futures) {
            future.get();
        }

        // 10 successful transfers of 100 from wallet 1 (1000 - 10*100 = 0)
        Wallet w1 = walletRepository.findById("wallet_1").orElseThrow();
        Wallet w2 = walletRepository.findById("wallet_2").orElseThrow();

        assertEquals(0, new BigDecimal("0.00").compareTo(w1.getBalance()));
        assertEquals(0, new BigDecimal("1500.00").compareTo(w2.getBalance()));
    }

    @Test
    void testIdempotencyConflict() throws Exception {
        org.springframework.beans.factory.BeanFactory beanFactory = org.springframework.web.context.support.WebApplicationContextUtils.getRequiredWebApplicationContext(mockMvc.getDispatcherServlet().getServletContext());
        com.wallet.transfer.repository.IdempotencyRecordRepository repo = beanFactory.getBean(com.wallet.transfer.repository.IdempotencyRecordRepository.class);
        
        com.wallet.transfer.entity.IdempotencyRecord record = com.wallet.transfer.entity.IdempotencyRecord.builder()
                .idempotencyKey("idem-conflict-123")
                .requestPayload("{}")
                .build();
        repo.save(record);

        TransferRequest request = new TransferRequest();
        request.setIdempotencyKey("idem-conflict-123");
        request.setFromWalletId("wallet_1");
        request.setToWalletId("wallet_2");
        request.setAmount(new BigDecimal("100.00"));

        mockMvc.perform(post("/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isConflict())
                .andExpect(jsonPath("$.error").value("Conflict"))
                .andExpect(jsonPath("$.message").value("A request with this idempotency key is currently being processed."));
    }
}
