package com.kullu.wallet.support;

import org.springframework.boot.test.util.TestPropertyValues;
import org.springframework.context.ApplicationContextInitializer;
import org.springframework.context.ConfigurableApplicationContext;
import org.testcontainers.containers.PostgreSQLContainer;
import org.testcontainers.utility.DockerImageName;

public class PostgresContainerInitializer
    implements ApplicationContextInitializer<ConfigurableApplicationContext> {

    private static final PostgreSQLContainer<?> POSTGRES;

    static {
        POSTGRES = new PostgreSQLContainer<>(DockerImageName.parse("postgres:16-alpine"))
            .withDatabaseName("wallet")
            .withUsername("wallet")
            .withPassword("wallet")
            .withReuse(false);
        POSTGRES.start();
        Runtime.getRuntime().addShutdownHook(new Thread(POSTGRES::stop));
    }

    public static PostgreSQLContainer<?> container() {
        return POSTGRES;
    }

    @Override
    public void initialize(ConfigurableApplicationContext ctx) {
        TestPropertyValues.of(
            "spring.datasource.url=" + POSTGRES.getJdbcUrl(),
            "spring.datasource.username=" + POSTGRES.getUsername(),
            "spring.datasource.password=" + POSTGRES.getPassword(),
            "spring.datasource.driver-class-name=org.postgresql.Driver",
            "spring.jpa.hibernate.ddl-auto=validate",
            "spring.jpa.properties.hibernate.dialect=org.hibernate.dialect.PostgreSQLDialect",
            "spring.flyway.enabled=true"
        ).applyTo(ctx.getEnvironment());
    }
}
