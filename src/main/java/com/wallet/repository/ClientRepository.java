package com.wallet.repository;

import com.wallet.domain.Client;
import org.jooq.DSLContext;
import org.jooq.Record;
import org.springframework.stereotype.Repository;

import java.time.OffsetDateTime;
import java.util.Optional;

import static com.wallet.jooq.Tables.CLIENTS;

/**
 * Read-only persistence for the {@code clients} registry; registration is an
 * out-of-band operation (DB seed / migration).
 */
@Repository
public class ClientRepository {

    private final DSLContext dsl;

    public ClientRepository(DSLContext dsl) {
        this.dsl = dsl;
    }

    public boolean existsById(String id) {
        return dsl.fetchExists(
                dsl.selectOne()
                        .from(CLIENTS)
                        .where(CLIENTS.ID.eq(id))
        );
    }

    public Optional<Client> findById(String id) {
        return dsl.selectFrom(CLIENTS)
                .where(CLIENTS.ID.eq(id))
                .fetchOptional()
                .map(this::mapRecordToClient);
    }

    private Client mapRecordToClient(Record r) {
        if (r == null) return null;
        OffsetDateTime createdAt = r.get(CLIENTS.CREATED_AT);
        return new Client(
                r.get(CLIENTS.ID),
                r.get(CLIENTS.NAME),
                createdAt != null ? createdAt.toInstant() : null
        );
    }
}
