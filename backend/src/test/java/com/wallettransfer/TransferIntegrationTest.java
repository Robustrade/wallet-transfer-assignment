package com.wallettransfer;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallettransfer.ledger.repository.LedgerEntryRepository;
import com.wallettransfer.transfer.dto.CreateTransferRequest;
import com.wallettransfer.wallet.domain.Wallet;
import com.wallettransfer.wallet.repository.WalletRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;

@SpringBootTest
@AutoConfigureMockMvc
class TransferIntegrationTest {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private ObjectMapper objectMapper;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @BeforeEach
    void resetWallets() {
        ledgerEntryRepository.deleteAll();
        walletRepository.deleteAll();
        walletRepository.save(new Wallet("wallet_1", 1000));
        walletRepository.save(new Wallet("wallet_2", 500));
    }

    @Test
    void createsTransferWithLedgerEntries() throws Exception {
        CreateTransferRequest request =
                new CreateTransferRequest("key-1", "wallet_1", "wallet_2", 100L);

        mockMvc.perform(post("/api/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.status").value("PROCESSED"))
                .andExpect(jsonPath("$.duplicate").value(false));

        assertThat(walletRepository.findById("wallet_1").orElseThrow().getBalance()).isEqualTo(900);
        assertThat(walletRepository.findById("wallet_2").orElseThrow().getBalance()).isEqualTo(600);
        assertThat(ledgerEntryRepository.count()).isEqualTo(2);
    }

    @Test
    void duplicateIdempotencyKeyReturnsSameResultWithoutDoubleSpend() throws Exception {
        CreateTransferRequest request =
                new CreateTransferRequest("dup-key", "wallet_1", "wallet_2", 50L);
        String body = objectMapper.writeValueAsString(request);

        mockMvc.perform(post("/api/transfers").contentType(MediaType.APPLICATION_JSON).content(body))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.duplicate").value(false));

        mockMvc.perform(post("/api/transfers").contentType(MediaType.APPLICATION_JSON).content(body))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.duplicate").value(true))
                .andExpect(jsonPath("$.status").value("PROCESSED"));

        assertThat(walletRepository.findById("wallet_1").orElseThrow().getBalance()).isEqualTo(950);
        assertThat(ledgerEntryRepository.count()).isEqualTo(2);
    }

    @Test
    void failsWhenBalanceIsInsufficient() throws Exception {
        CreateTransferRequest request =
                new CreateTransferRequest("fail-key", "wallet_1", "wallet_2", 5000L);

        mockMvc.perform(post("/api/transfers")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isUnprocessableEntity())
                .andExpect(jsonPath("$.status").value("FAILED"));

        assertThat(walletRepository.findById("wallet_1").orElseThrow().getBalance()).isEqualTo(1000);
        assertThat(ledgerEntryRepository.count()).isZero();
    }

    @Test
    void listsWallets() throws Exception {
        mockMvc.perform(get("/api/wallets"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$[0].id").exists())
                .andExpect(jsonPath("$[0].balance").exists());
    }
}
