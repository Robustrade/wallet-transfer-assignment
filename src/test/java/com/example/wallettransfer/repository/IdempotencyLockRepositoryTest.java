package com.example.wallettransfer.repository;

import org.junit.jupiter.api.Test;
import org.springframework.jdbc.core.JdbcTemplate;

import java.util.List;

import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

class IdempotencyLockRepositoryTest {

    @Test
    void acquiresTransactionScopedLockForTheFullIdempotencyKey() {
        JdbcTemplate jdbcTemplate = mock(JdbcTemplate.class);
        when(jdbcTemplate.queryForList("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "transfer-key"))
                .thenReturn(List.of());
        IdempotencyLockRepository repository = new IdempotencyLockRepository(jdbcTemplate);

        repository.lock("transfer-key");

        verify(jdbcTemplate).queryForList(eq("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))"), eq("transfer-key"));
    }
}
