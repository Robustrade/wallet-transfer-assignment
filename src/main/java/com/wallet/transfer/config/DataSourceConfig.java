package com.wallet.transfer.config;

import com.zaxxer.hikari.HikariConfig;
import com.zaxxer.hikari.HikariDataSource;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.sqlite.SQLiteConfig;
import org.sqlite.SQLiteDataSource;

import javax.sql.DataSource;

@Configuration
public class DataSourceConfig {

    @Value("${spring.datasource.url}")
    private String url;

    @Bean
    public DataSource dataSource() {
        SQLiteConfig sqliteConfig = new SQLiteConfig();
        sqliteConfig.setBusyTimeout(30_000);
        sqliteConfig.setJournalMode(SQLiteConfig.JournalMode.WAL);
        sqliteConfig.enforceForeignKeys(true);

        SQLiteDataSource sqliteDataSource = new SQLiteDataSource(sqliteConfig);
        sqliteDataSource.setUrl(url);

        HikariConfig hikariConfig = new HikariConfig();
        hikariConfig.setDataSource(sqliteDataSource);

        // SQLite's WAL mode allows multiple concurrent readers and serializes writers via its
        // own write-lock, so correctness does not depend on limiting the pool to 1 connection.
        // A pool > 1 is also required so that REQUIRES_NEW transactions (IdempotencyService)
        // can acquire a second connection without deadlocking the suspended outer transaction.
        hikariConfig.setMaximumPoolSize(10);
        hikariConfig.setConnectionTestQuery("SELECT 1");

        return new HikariDataSource(hikariConfig);
    }
}
